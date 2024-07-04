package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// Set up as http.RoundTripper that can retry, add auth in future, etc.
type ClientTransport struct {
	inner   http.RoundTripper
	retries int
	backoff []time.Duration
}

func NewClientTransport(inner http.RoundTripper) *ClientTransport {
	return &ClientTransport{
		inner:   inner,
		retries: 2,
		backoff: []time.Duration{1 * time.Second, 3 * time.Second},
	}
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
		for i := 0; i < t.retries; i++ {
			log.Debug().Int("retry_count", i+1).
				Interface("backoff_seconds", t.backoff[i]).
				Str("url", req.URL.String()).
				Msg("Retrying HTTP Request")
			// first try already failed so wait before retrying
			time.Sleep(t.backoff[i])
			// re-add body to request
			if req.Body != nil {
				req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
			resp, err = t.inner.RoundTrip(req)
			if err == nil && resp.StatusCode < 500 {
				return resp, nil
			}
		}
		if err != nil {
			return nil, fmt.Errorf("error sending HTTP Request: %w", err)
		} else {
			return nil, fmt.Errorf("received Server Error Status Code: %d", resp.StatusCode)
		}
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
