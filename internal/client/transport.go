package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ClientTransportOption is a functional option for NewClientTransport.
type ClientTransportOption func(*ClientTransport)

// WithBackoff sets the retry backoff durations. The number of retries equals
// len(durations). Default: [1s, 3s] (2 retries).
func WithBackoff(durations []time.Duration) ClientTransportOption {
	return func(t *ClientTransport) {
		t.backoff = durations
	}
}

// WithAfter injects the timer function used for retry backoff, replacing
// time.After. Intended for tests to avoid real wall-clock delays.
func WithAfter(fn func(time.Duration) <-chan time.Time) ClientTransportOption {
	return func(t *ClientTransport) {
		t.after = fn
	}
}

// WithLogger injects the logger the transport uses. Defaults to log.Logger.
func WithLogger(logger zerolog.Logger) ClientTransportOption {
	return func(t *ClientTransport) {
		t.logger = logger
	}
}

// Set up as http.RoundTripper that can retry, add auth in future, etc.
type ClientTransport struct {
	inner   http.RoundTripper
	backoff []time.Duration
	after   func(time.Duration) <-chan time.Time
	logger  zerolog.Logger
}

// NewClientTransport constructs a ClientTransport wrapping inner.
// Default: 2 retries with backoff [1s, 3s], using time.After.
// The existing call site NewClientTransport(baseTransport) keeps working unchanged.
func NewClientTransport(inner http.RoundTripper, opts ...ClientTransportOption) *ClientTransport {
	t := &ClientTransport{
		inner:   inner,
		backoff: []time.Duration{1 * time.Second, 3 * time.Second},
		after:   time.After,
		logger:  log.Logger,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// drainClose drains and closes a response body that the transport is about to
// discard (an error/retry path where the response is not returned to the caller).
// Without this the underlying connection is never returned to the pool and leaks,
// since a RoundTripper that returns a nil response gives the http.Client nothing
// to close. nil-safe.
func drainClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// middleware for http to handle retries
func (t *ClientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// read it first since once sent on first try it will be already streamed
	// for request with body, we need to ensure we can re-use the body after it is read/cleared from the buffer on first request
	// so copy it first to a buffer
	var bodyBytes []byte
	// for post/put (usually) we need to read the body and re-use it on retries
	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
		// `io.NopCloser()` is used to wrap the buffer and provide a `Close()` function for `io.ReaderClose` functionality.
		// `req.Body` is then set to the copied data held in the buffer created with the `bodyBytes` slice.
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}
	// `req.Body` is streamed when used in `RoundTrip()`, so we need to re-create the `req.Body` with the copied data when retrying
	resp, err := t.inner.RoundTrip(req)
	if err != nil || resp.StatusCode >= 500 {
		// retries = len(backoff); index i is always in range — no panic possible.
		recovered := false
		for i, backoffDur := range t.backoff {
			t.logger.Debug().Int("retry_count", i+1).
				Interface("backoff_seconds", backoffDur).
				Str("url", req.URL.String()).
				Msg("Retrying HTTP Request")
			// Free the previous failed response before retrying so its connection
			// returns to the pool instead of leaking (nil-safe on the first
			// iteration when the initial attempt errored with no response).
			drainClose(resp)
			// first try already failed so wait before retrying — but abort the
			// wait immediately if the request's context is cancelled (shutdown
			// or Prometheus scrape timeout); retrying with a dead context is
			// pointless and delays shutdown by the full backoff.
			select {
			case <-req.Context().Done():
				return nil, fmt.Errorf("aborting retries - request context done: %w", req.Context().Err())
			case <-t.after(backoffDur):
			}
			// re-add body to request
			if req.Body != nil {
				req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
			resp, err = t.inner.RoundTrip(req)
			if err == nil && resp.StatusCode < 500 {
				// Got a non-5xx response. Don't short-circuit to success here:
				// a 4xx/3xx still has to go through the same classification below
				// as a first-attempt response would. Break and fall through.
				recovered = true
				break
			}
		}
		if !recovered {
			// every attempt errored or returned 5xx
			if err != nil {
				return nil, fmt.Errorf("error sending HTTP Request: %w", err)
			}
			drainClose(resp)
			return nil, fmt.Errorf("received Server Error Status Code: %d", resp.StatusCode)
		}
		// fall through: resp is now a <500 response, classify it like any other
	}
	if resp.StatusCode >= 400 && resp.StatusCode <= 499 {
		t.logger.Error().Int("status_code", resp.StatusCode).Str("url", req.URL.String()).Msgf("Received 40X Status Code: %d", resp.StatusCode)
		drainClose(resp)
		return nil, fmt.Errorf("received 40x Status Code: %d", resp.StatusCode)
	}
	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		t.logger.Debug().Int("status_code", resp.StatusCode).Str("url", req.URL.String()).Msgf("Received 30X Status Code: %d", resp.StatusCode)
		// Location() only reads headers, so it is safe to read before closing the body.
		location, locErr := resp.Location()
		drainClose(resp)
		if locErr == nil {
			return nil, fmt.Errorf("received Redirect Status Code: %d, Location: %s", resp.StatusCode, location.String())
		}
		return nil, fmt.Errorf("received Redirect Status Code: %d, ", resp.StatusCode)
	}
	return resp, nil
}
