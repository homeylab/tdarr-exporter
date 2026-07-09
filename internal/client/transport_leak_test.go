package client

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// trackedBody records whether Close was called on a response body.
type trackedBody struct {
	io.Reader
	closed *atomic.Bool
}

func (b trackedBody) Close() error {
	b.closed.Store(true)
	return nil
}

// seqRoundTripper returns one response per call from a fixed status sequence
// (repeating the last status once exhausted) and records each response body's
// close state so a test can assert no discarded body is leaked.
type seqRoundTripper struct {
	statuses []int
	idx      int
	closes   []*atomic.Bool
}

func (s *seqRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	code := s.statuses[s.idx]
	if s.idx < len(s.statuses)-1 {
		s.idx++
	}
	closed := &atomic.Bool{}
	s.closes = append(s.closes, closed)
	return &http.Response{
		StatusCode: code,
		Body:       trackedBody{Reader: strings.NewReader("body"), closed: closed},
		Header:     make(http.Header),
	}, nil
}

// TestRoundTrip_ClosesDiscardedBodies verifies that every response the transport
// discards on an error/retry path has its body closed, so connections are not
// leaked when Tdarr returns 5xx/4xx/3xx.
func TestRoundTrip_ClosesDiscardedBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statuses   []int
		wantBodies int
	}{
		{"4xx", []int{404}, 1},
		{"3xx", []int{301}, 1},
		{"5xx exhausted", []int{500, 500}, 2},
		{"5xx then 4xx", []int{500, 404}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rt := &seqRoundTripper{statuses: tt.statuses}
			transport := NewClientTransport(rt,
				WithBackoff([]time.Duration{0}), // one retry, no real delay
				WithAfter(immediateAfter),
			)

			req, err := http.NewRequest(http.MethodGet, "http://example.test", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			resp, rtErr := transport.RoundTrip(req)
			if rtErr == nil {
				t.Fatalf("expected error for statuses %v, got response %+v", tt.statuses, resp)
			}
			if resp != nil {
				t.Fatalf("expected nil response on error, got %+v", resp)
			}
			if len(rt.closes) != tt.wantBodies {
				t.Fatalf("expected %d response bodies created, got %d", tt.wantBodies, len(rt.closes))
			}
			for i, c := range rt.closes {
				if !c.Load() {
					t.Errorf("response body %d was not closed (connection leak)", i)
				}
			}
		})
	}
}
