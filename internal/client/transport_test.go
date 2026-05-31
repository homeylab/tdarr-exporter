package client

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeRoundTripper records calls and returns pre-configured responses/errors.
type fakeRoundTripper struct {
	responses []fakeResponse
	calls     int
}

type fakeResponse struct {
	statusCode int
	err        error
	// captureBody captures what was read from req.Body on this call (for POST body reuse tests).
	captureBody *string
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.calls >= len(f.responses) {
		return nil, fmt.Errorf("fakeRoundTripper: unexpected call %d (configured %d)", f.calls+1, len(f.responses))
	}
	r := f.responses[f.calls]
	f.calls++
	// Capture body if the test asked for it.
	if r.captureBody != nil && req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		*r.captureBody = string(b)
		req.Body = io.NopCloser(strings.NewReader(string(b)))
	}
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{
		StatusCode: r.statusCode,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}

// noopSleep is a sleep func that never blocks and records nothing.
func noopSleep(_ time.Duration) {}

// collectingSleep returns a sleep func that appends durations to *out.
func collectingSleep(out *[]time.Duration) func(time.Duration) {
	return func(d time.Duration) {
		*out = append(*out, d)
	}
}

func newRequest(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	return req
}

func TestClientTransport_RetryBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		responses     []fakeResponse
		backoff       []time.Duration
		wantCallCount int
		wantSleeps    []time.Duration
		wantErrSubstr string // non-empty: expect error containing this string
		wantStatus    int    // 0 means we expect an error (no response)
	}{
		{
			name: "success on first try — no retries, no sleep",
			responses: []fakeResponse{
				{statusCode: 200},
			},
			backoff:       []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantCallCount: 1,
			wantSleeps:    nil,
			wantStatus:    200,
		},
		{
			name: "5xx then 200 — one retry with first backoff sleep",
			responses: []fakeResponse{
				{statusCode: 500},
				{statusCode: 200},
			},
			backoff:       []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantCallCount: 2,
			wantSleeps:    []time.Duration{10 * time.Millisecond},
			wantStatus:    200,
		},
		{
			name: "5xx exhausting all retries — returns error",
			responses: []fakeResponse{
				{statusCode: 500},
				{statusCode: 500},
				{statusCode: 500},
			},
			backoff:       []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantCallCount: 3, // 1 initial + 2 retries
			wantSleeps:    []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantErrSubstr: "Server Error Status Code: 500",
		},
		{
			name: "4xx — immediate error, no retry",
			responses: []fakeResponse{
				{statusCode: 404},
			},
			backoff:       []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantCallCount: 1,
			wantSleeps:    nil,
			wantErrSubstr: "40x Status Code: 404",
		},
		{
			name: "3xx redirect — error, no retry",
			responses: []fakeResponse{
				{statusCode: 301},
			},
			backoff:       []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantCallCount: 1,
			wantSleeps:    nil,
			wantErrSubstr: "Redirect Status Code: 301",
		},
		{
			name: "retries > len(backoff) — no panic, retries clamped to len(backoff)",
			responses: []fakeResponse{
				{statusCode: 500},
				{statusCode: 500},
			},
			// Only 1 backoff entry: expect exactly 1 retry (1 sleep), then error.
			backoff:       []time.Duration{10 * time.Millisecond},
			wantCallCount: 2,
			wantSleeps:    []time.Duration{10 * time.Millisecond},
			wantErrSubstr: "Server Error Status Code: 500",
		},
		{
			name: "network error then success — one retry",
			responses: []fakeResponse{
				{err: fmt.Errorf("connection refused")},
				{statusCode: 200},
			},
			backoff:       []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantCallCount: 2,
			wantSleeps:    []time.Duration{10 * time.Millisecond},
			wantStatus:    200,
		},
		{
			name: "network error exhausting retries — error wrapped",
			responses: []fakeResponse{
				{err: fmt.Errorf("timeout")},
				{err: fmt.Errorf("timeout")},
				{err: fmt.Errorf("timeout")},
			},
			backoff:       []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantCallCount: 3,
			wantSleeps:    []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantErrSubstr: "error sending HTTP Request",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var sleeps []time.Duration
			inner := &fakeRoundTripper{responses: tc.responses}
			tr := NewClientTransport(inner, WithBackoff(tc.backoff), WithSleep(collectingSleep(&sleeps)))

			req := newRequest(t, http.MethodGet, "http://example.com/test", "")
			resp, err := tr.RoundTrip(req)

			// Verify call count.
			if inner.calls != tc.wantCallCount {
				t.Errorf("call count: want %d, got %d", tc.wantCallCount, inner.calls)
			}

			// Verify sleep durations.
			if len(sleeps) != len(tc.wantSleeps) {
				t.Errorf("sleep calls: want %v, got %v", tc.wantSleeps, sleeps)
			} else {
				for i, want := range tc.wantSleeps {
					if sleeps[i] != want {
						t.Errorf("sleep[%d]: want %v, got %v", i, want, sleeps[i])
					}
				}
			}

			// Verify error presence/absence.
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Errorf("want error containing %q, got nil (status %d)", tc.wantErrSubstr, resp.StatusCode)
				} else if !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Errorf("want error containing %q, got %q", tc.wantErrSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("want no error, got: %v", err)
				}
				if resp == nil {
					t.Fatal("want non-nil response")
				}
				if resp.StatusCode != tc.wantStatus {
					t.Errorf("status: want %d, got %d", tc.wantStatus, resp.StatusCode)
				}
			}
		})
	}
}

// TestClientTransport_PostBodyReusedAcrossRetries verifies that POST body bytes
// are readable on every retry attempt, not just the first.
func TestClientTransport_PostBodyReusedAcrossRetries(t *testing.T) {
	t.Parallel()

	body1 := ""
	body2 := ""
	body3 := ""
	inner := &fakeRoundTripper{
		responses: []fakeResponse{
			{statusCode: 500, captureBody: &body1},
			{statusCode: 500, captureBody: &body2},
			{statusCode: 200, captureBody: &body3},
		},
	}
	tr := NewClientTransport(inner,
		WithBackoff([]time.Duration{0, 0}),
		WithSleep(noopSleep),
	)

	req := newRequest(t, http.MethodPost, "http://example.com/post", `{"key":"value"}`)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	want := `{"key":"value"}`
	for i, got := range []string{body1, body2, body3} {
		if got != want {
			t.Errorf("attempt %d body: want %q, got %q", i+1, want, got)
		}
	}
}

// TestClientTransport_DefaultsArePreserved verifies the default sleep function is
// wired up and the default backoff is [1s, 3s], without invoking a real sleep.
func TestClientTransport_DefaultsArePreserved(t *testing.T) {
	t.Parallel()

	inner := &fakeRoundTripper{responses: []fakeResponse{{statusCode: 200}}}
	tr := NewClientTransport(inner) // no options — use defaults

	if tr.sleep == nil {
		t.Error("default sleep should be non-nil")
	}
	// Default backoff should be [1s, 3s].
	want := []time.Duration{1 * time.Second, 3 * time.Second}
	if len(tr.backoff) != len(want) {
		t.Fatalf("default backoff len: want %d, got %d", len(want), len(tr.backoff))
	}
	for i, d := range want {
		if tr.backoff[i] != d {
			t.Errorf("default backoff[%d]: want %v, got %v", i, d, tr.backoff[i])
		}
	}

	// Smoke-test: should succeed without panic.
	req := newRequest(t, http.MethodGet, "http://example.com", "")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
}
