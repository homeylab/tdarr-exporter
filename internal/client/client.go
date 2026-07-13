package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Client struct is an *Arr client.
type RequestClient struct {
	httpClient http.Client
	apiKey     string
	URL        url.URL
	// logger is the client's logger, defaulting to the package-global log.Logger.
	// Injected (not read from the global at each call) so tests can silence or
	// capture client logs deterministically.
	logger zerolog.Logger
}

type QueryParams = url.Values

// NewRequestClient constructs an HTTP client for Tdarr requests.
//   - verifySsl: when true, TLS certificates are verified (InsecureSkipVerify=false).
//   - timeoutSeconds: HTTP client timeout; use config.HttpTimeoutSeconds (default 15).
//
// The global http.DefaultTransport is never mutated; a fresh clone is created per call.
func NewRequestClient(parsedUrl *url.URL, verifySsl bool, timeoutSeconds int, apiKeyAuth string) *RequestClient {
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	baseTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: !verifySsl}

	return &RequestClient{
		httpClient: http.Client{
			// If CheckRedirect is nil, the Client uses its default policy,
			// which is to stop after 10 consecutive requests.
			// uncomment below to not follow redirects
			// CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 	return http.ErrUseLastResponse
			// },
			// TdarrTransport implements `RoundTrip`
			Transport: NewClientTransport(baseTransport),
			Timeout:   time.Duration(timeoutSeconds) * time.Second,
		},
		URL:    *parsedUrl,
		apiKey: apiKeyAuth,
		logger: log.Logger,
	}
}

// maxBodyHeadBytes bounds how much of a failed response body is captured for the
// decode-failure diagnostic. 2048 matches Kubernetes client-go's
// maxUnstructuredResponseTextBytes — enough to cover a typical non-JSON error
// page's identifying head (doctype, title, status banner) without spamming logs.
const maxBodyHeadBytes = 2048

// cappedWriter keeps only the first limit bytes written to it, discarding the
// rest. It always reports a full-length write so an io.TeeReader driving it never
// sees a short write and stalls the read.
type cappedWriter struct {
	buf   *bytes.Buffer
	limit int
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if remaining := w.limit - w.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			w.buf.Write(p[:remaining])
		} else {
			w.buf.Write(p)
		}
	}
	return len(p), nil
}

// unmarshalBody decodes a JSON response body into target. A panic inside a
// scrape (e.g. from a pathological reader) is already converted to tdarr_up=0
// by the collector's Collect recover, so no recover is needed here.
//
// On decode failure it debug-logs the head of the body: the common real failure
// is a non-JSON response (wrong URL, reverse proxy, auth error → HTML/text page),
// and its first bytes instantly distinguish that from JSON schema drift. Kept at
// debug level because the decode error is already logged upstream at error level.
//
// The head capture is gated on debug being enabled: when it is off we decode
// straight from the reader with no extra buffering, so the success path pays
// nothing in production. When on, a capped tee buffers only the head.
func (c *RequestClient) unmarshalBody(body io.Reader, target any) error {
	var head bytes.Buffer
	reader := body
	if c.logger.Debug().Enabled() {
		reader = io.TeeReader(body, &cappedWriter{buf: &head, limit: maxBodyHeadBytes})
	}
	if err := json.NewDecoder(reader).Decode(target); err != nil {
		c.logger.Debug().Bytes("body_head", head.Bytes()).Msg("failed to decode response body")
		return fmt.Errorf("failed to decode response body: %w", err)
	}
	return nil
}

// DoRequest - Take a HTTP Request and return Unmarshaled data. The ctx is
// attached to the request so a cancelled/expired context aborts it in flight.
func (c *RequestClient) DoRequest(ctx context.Context, path string, target any, queryParams ...QueryParams) error {
	values := c.URL.Query()
	// add query params
	for _, m := range queryParams {
		for key, vals := range m {
			for _, val := range vals {
				values.Add(key, val)
			}
		}
	}
	url := c.URL.JoinPath(path)
	url.RawQuery = values.Encode()

	c.logger.Debug().Str("url", url.String()).Msg("Sending HTTP request")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		c.logger.Error().Err(err).Str("url", url.String()).Msg("Failed to create HTTP Request")
		return fmt.Errorf("failed to create HTTP Request(%s): %w", url, err)
	}
	if c.apiKey != "" {
		c.logger.Debug().Str("authHeaderField", "x-api-key").Msg("Setting Authorization header - api token is set")
		req.Header.Set("x-api-key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP Request(%s): %w", url, err)
	}

	defer func() {
		if cErr := resp.Body.Close(); cErr != nil {
			c.logger.Error().Err(cErr).Msg("Failed to close response body")
		}
	}()
	return c.unmarshalBody(resp.Body, target)
}

// DoPostRequest - Take a HTTP Request and return Unmarshaled data. The ctx is
// attached to the request so a cancelled/expired context aborts it in flight.
func (c *RequestClient) DoPostRequest(ctx context.Context, path string, target any, payload []byte) error {
	url := c.URL.JoinPath(path)
	c.logger.Debug().Str("url", url.String()).Msg("Sending HTTP POST request")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url.String(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP Request(%s): %w", url, err)
	}

	// json content
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		c.logger.Debug().Str("authHeaderField", "x-api-key").Msg("Setting Authorization header - api token is set")
		req.Header.Set("x-api-key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP Request(%s): %w", url, err)
	}
	defer func() {
		if cErr := resp.Body.Close(); cErr != nil {
			c.logger.Error().Err(cErr).Msg("Failed to close response body")
		}
	}()
	return c.unmarshalBody(resp.Body, target)
}
