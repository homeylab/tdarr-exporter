package client

import (
	"fmt"
	"net/http"
)

// Set up as http.RoundTripper that can retry, add auth in future, etc.
type TdarrTransport struct {
	inner http.RoundTripper
}

func NewTdarrTransport(inner http.RoundTripper) *TdarrTransport {
	return &TdarrTransport{
		inner: inner,
	}
}

// middleware for http to handle retries
func (t *TdarrTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.inner.RoundTrip(req)
	if err != nil || resp.StatusCode >= 500 {
		retries := 2
		for i := 0; i < retries; i++ {
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
		return nil, fmt.Errorf("received Client Error Status Code: %d", resp.StatusCode)
	}
	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		if location, err := resp.Location(); err == nil {
			return nil, fmt.Errorf("received Redirect Status Code: %d, Location: %s", resp.StatusCode, location.String())
		} else {
			return nil, fmt.Errorf("received Redirect Status Code: %d, ", resp.StatusCode)
		}
	}
	return resp, nil
}
