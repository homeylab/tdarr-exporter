package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Client struct is an *Arr client.
type RequestClient struct {
	httpClient http.Client
	apiKey     string
	URL        url.URL
}

type QueryParams = url.Values

func NewRequestClient(parsedUrl *url.URL, insecureSkipVerify bool, apiKeyAuth string) (*RequestClient, error) {
	baseTransport := http.DefaultTransport
	baseTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: !insecureSkipVerify}

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
			Timeout:   time.Duration(time.Duration(15) * time.Second),
		},
		URL:    *parsedUrl,
		apiKey: apiKeyAuth,
	}, nil
}

func (c *RequestClient) unmarshalBody(body io.Reader, target interface{}) (err error) {
	// return error instead of panic
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from panic: %s", r)
			// if debug, log body
			if log.Logger.GetLevel() == zerolog.DebugLevel {
				// try to copy io.Reader to string for troubleshooting
				s := new(strings.Builder)
				_, copyErr := io.Copy(s, body)
				if copyErr != nil {
					log.Error().Err(copyErr).Interface("recover", r).Msg("Failed to copy body to string in recover for troubleshooting")
				}
				log.Error().Str("body", s.String()).Msg("Problem body")
			}
			log.Error().Err(err).Interface("recover", r).Msg("Recovered while unmarshalling response")
		}
	}()
	// read body into target
	err = json.NewDecoder(body).Decode(target)
	return
}

// DoRequest - Take a HTTP Request and return Unmarshaled data
func (c *RequestClient) DoRequest(path string, target interface{}, queryParams ...QueryParams) error {
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

	log.Debug().Str("url", url.String()).Msg("Sending HTTP request")
	req, err := http.NewRequest(http.MethodGet, url.String(), nil)
	if err != nil {
		log.Error().Err(err).Str("url", url.String()).Msg("Failed to create HTTP Request")
		return fmt.Errorf("failed to create HTTP Request(%s): %w", url, err)
	}
	if c.apiKey != "" {
		log.Debug().Str("authHeaderField", "x-api-key").Msg("Setting Authorization header - api token is set")
		req.Header.Set("x-api-key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP Request(%s): %w", url, err)
	}

	defer resp.Body.Close()
	return c.unmarshalBody(resp.Body, target)
}

// DoRequest - Take a HTTP Request and return Unmarshaled data
func (c *RequestClient) DoPostRequest(path string, target interface{}, payload []byte) error {
	url := c.URL.JoinPath(path)
	log.Debug().Str("url", url.String()).Msg("Sending HTTP POST request")
	req, err := http.NewRequest(http.MethodPost, url.String(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP Request(%s): %w", url, err)
	}

	// json content
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		log.Debug().Str("authHeaderField", "x-api-key").Msg("Setting Authorization header - api token is set")
		req.Header.Set("x-api-key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP Request(%s): %w", url, err)
	}
	defer resp.Body.Close()
	return c.unmarshalBody(resp.Body, target)
}
