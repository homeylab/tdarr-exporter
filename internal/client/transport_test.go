package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// A lot is tested already in `client_test.go`
// focus on `RoundTrip` functionality
func TestNewTransport(t *testing.T) {
	require := require.New(t)
	client := NewClientTransport(NewBaseTransport(false))
	require.NotNil(client, "NewClientTransport should return a client")
	require.Greater(client.retries, 0)
	require.Greater(len(client.backoff), 0)
}

func TestErrorCodes(t *testing.T) {
	client := NewClientTransport(NewBaseTransport(false))

	parameters := []struct {
		name string
		code int
	}{
		{
			name: "500code",
			code: 500,
		},
		{
			name: "400code",
			code: 400,
		},
		{
			name: "300code",
			code: 300,
		},
	}

	for _, param := range parameters {
		t.Run(param.name, func(t *testing.T) {
			require := require.New(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(param.code)
			}))
			defer ts.Close()
			req, _ := http.NewRequest("GET", ts.URL, nil)
			_, err := client.RoundTrip(req)
			require.NotNil(err, "RoundTrip should return an error")
		})
	}
}

func TestValidCode(t *testing.T) {
	client := NewClientTransport(NewBaseTransport(false))

	parameters := []struct {
		name string
		code int
	}{
		{
			name: "200code",
			code: 200,
		},
		{
			name: "201code",
			code: 201,
		},
	}

	for _, param := range parameters {
		t.Run(param.name, func(t *testing.T) {
			require := require.New(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(param.code)
			}))
			defer ts.Close()
			req, _ := http.NewRequest("GET", ts.URL, nil)
			resp, err := client.RoundTrip(req)
			require.Nil(err, "RoundTrip should not return an error")
			require.IsType(&http.Response{}, resp)
		})
	}
}
