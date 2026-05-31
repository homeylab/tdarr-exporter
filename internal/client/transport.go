package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

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

// WithSleep injects a custom sleep function, replacing time.Sleep.
// Intended for tests to avoid real wall-clock delays.
func WithSleep(fn func(time.Duration)) ClientTransportOption {
	return func(t *ClientTransport) {
		t.sleep = fn
	}
}

// Set up as http.RoundTripper that can retry, add auth in future, etc.
type ClientTransport struct {
	inner   http.RoundTripper
	backoff []time.Duration
	sleep   func(time.Duration)
}

// NewClientTransport constructs a ClientTransport wrapping inner.
// Default: 2 retries with backoff [1s, 3s], using time.Sleep.
// The existing call site NewClientTransport(baseTransport) keeps working unchanged.
func NewClientTransport(inner http.RoundTripper, opts ...ClientTransportOption) *ClientTransport {
	t := &ClientTransport{
		inner:   inner,
		backoff: []time.Duration{1 * time.Second, 3 * time.Second},
		sleep:   time.Sleep,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
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
			log.Debug().Int("retry_count", i+1).
				Interface("backoff_seconds", backoffDur).
				Str("url", req.URL.String()).
				Msg("Retrying HTTP Request")
			// first try already failed so wait before retrying
			t.sleep(backoffDur)
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
			return nil, fmt.Errorf("received Server Error Status Code: %d", resp.StatusCode)
		}
		// fall through: resp is now a <500 response, classify it like any other
	}
	if resp.StatusCode >= 400 && resp.StatusCode <= 499 {
		log.Error().Int("status_code", resp.StatusCode).Str("url", req.URL.String()).Msgf("Received 40X Status Code: %d", resp.StatusCode)
		return nil, fmt.Errorf("received 40x Status Code: %d", resp.StatusCode)
	}
	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		log.Debug().Int("status_code", resp.StatusCode).Str("url", req.URL.String()).Msgf("Received 30X Status Code: %d", resp.StatusCode)
		if location, err := resp.Location(); err == nil {
			return nil, fmt.Errorf("received Redirect Status Code: %d, Location: %s", resp.StatusCode, location.String())
		} else {
			return nil, fmt.Errorf("received Redirect Status Code: %d, ", resp.StatusCode)
		}
	}
	return resp, nil
}
