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

	"github.com/rs/zerolog/log"
	"go.uber.org/zap"
)

// Client struct is an *Arr client.
type Client struct {
	httpClient http.Client
	URL        url.URL
}

type QueryParams = url.Values

func NewClient(baseUrl string, insecureSkipVerify bool, httpTimeoutSeconds int) (*Client, error) {
	u, err := url.Parse(baseUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL(%s): %w", baseUrl, err)
	}

	baseTransport := http.DefaultTransport
	baseTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: !insecureSkipVerify}

	return &Client{
		httpClient: http.Client{
			// If CheckRedirect is nil, the Client uses its default policy,
			// which is to stop after 10 consecutive requests.
			// uncomment below to not follow redirects
			// CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 	return http.ErrUseLastResponse
			// },
			// TdarrTransport implements `RoundTrip`
			Transport: NewTdarrTransport(baseTransport),
			Timeout:   time.Duration(time.Duration(httpTimeoutSeconds) * time.Second),
		},
		URL: *u,
	}, nil
}

func (c *Client) unmarshalBody(b io.Reader, target interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// return recovered panic as error
			err = fmt.Errorf("recovered from panic: %s", r)

			log := zap.S()
			if zap.S().Level() == zap.DebugLevel {
				s := new(strings.Builder)
				_, copyErr := io.Copy(s, b)
				if copyErr != nil {
					zap.S().Errorw("Failed to copy body to string in recover",
						"error", err, "recover", r)
				}
				log = log.With("body", s.String())
			}
			log.Errorw("Recovered while unmarshalling response", "error", r)

		}
	}()
	err = json.NewDecoder(b).Decode(target)
	return
}

// DoRequest - Take a HTTP Request and return Unmarshaled data
func (c *Client) DoRequest(path string, target interface{}, queryParams ...QueryParams) error {
	values := c.URL.Query()

	// merge all query params
	for _, m := range queryParams {
		for key, vals := range m {
			for _, val := range vals {
				values.Add(key, val)
			}
		}
	}

	url := c.URL.JoinPath(path)
	url.RawQuery = values.Encode()
	zap.S().Infow("Sending HTTP request",
		"url", url)

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP Request(%s): %w", url, err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP Request(%s): %w", url, err)
	}
	defer resp.Body.Close()
	return c.unmarshalBody(resp.Body, target)
}

// DoRequest - Take a HTTP Request and return Unmarshaled data
func (c *Client) DoPostRequest(path string, target interface{}, payload []byte) error {
	url := c.URL.JoinPath(path)
	fmt.Println(url.String())
	log.Info().Str("url", url.Host).Msg("Sending HTTP POST request")

	req, err := http.NewRequest(http.MethodPost, url.String(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP Request(%s): %w", url, err)
	}
	// json content
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP Request(%s): %w", url, err)
	}
	fmt.Println(resp)
	defer resp.Body.Close()
	return c.unmarshalBody(resp.Body, target)
}
