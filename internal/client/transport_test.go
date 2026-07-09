package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
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

// immediateAfter is an after func that fires immediately without blocking.
func immediateAfter(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

// collectingAfter returns an after func that appends durations to *out and
// fires immediately.
func collectingAfter(sleeps *[]time.Duration) func(time.Duration) <-chan time.Time {
	return func(d time.Duration) <-chan time.Time {
		*sleeps = append(*sleeps, d)
		ch := make(chan time.Time, 1)
		ch <- time.Time{}
		return ch
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
			// A 4xx arriving after a 5xx retry must be classified as an error, not
			// returned as a success response (regression guard for the post-retry
			// fall-through to the shared 4xx/3xx classification).
			name: "5xx then 4xx — classified as error, not returned as success",
			responses: []fakeResponse{
				{statusCode: 500},
				{statusCode: 404},
			},
			backoff:       []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantCallCount: 2,
			wantSleeps:    []time.Duration{10 * time.Millisecond},
			wantErrSubstr: "40x Status Code: 404",
		},
		{
			name: "5xx then 3xx — classified as redirect error, not returned as success",
			responses: []fakeResponse{
				{statusCode: 500},
				{statusCode: 302},
			},
			backoff:       []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantCallCount: 2,
			wantSleeps:    []time.Duration{10 * time.Millisecond},
			wantErrSubstr: "Redirect Status Code: 302",
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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var sleeps []time.Duration
			inner := &fakeRoundTripper{responses: tc.responses}
			tr := NewClientTransport(inner, WithBackoff(tc.backoff), WithAfter(collectingAfter(&sleeps)))

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
		WithAfter(immediateAfter),
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

	if tr.after == nil {
		t.Error("default after should be non-nil")
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

// roundTripFunc adapts a function to http.RoundTripper, for tests that need
// full control over every attempt (fakeRoundTripper replays a fixed list).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestClientTransport_ContextCancelDuringBackoff verifies that cancelling the
// request context while the transport is waiting out a retry backoff aborts
// the wait immediately (no further attempts, prompt ctx error) instead of
// sleeping the full backoff and retrying with a dead context.
func TestClientTransport_ContextCancelDuringBackoff(t *testing.T) {
	attempts := 0
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("dial refused")
	})

	ctx, cancel := context.WithCancel(context.Background())
	// after() that never fires and cancels the context instead: the only way
	// RoundTrip can return is via the ctx.Done() select arm.
	blockedAfter := func(time.Duration) <-chan time.Time {
		cancel()
		return make(chan time.Time) // never fires
	}

	tr := NewClientTransport(inner,
		WithBackoff([]time.Duration{time.Hour, time.Hour}),
		WithAfter(blockedAfter),
		WithLogger(zerolog.Nop()),
	)

	req := httptest.NewRequest(http.MethodGet, "http://tdarr.test/api", nil).WithContext(ctx)
	resp, err := tr.RoundTrip(req) //nolint:bodyclose // resp is nil on error
	if resp != nil {
		t.Fatalf("expected nil response, got %v", resp)
	}
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled in error chain, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts: want 1 (no retry after cancel), got %d", attempts)
	}
}
