package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	// static input
	u := "http://localhost"
	fakeApiKey := "fakeApiKey"
	// test
	httpUrl, _ := url.Parse(u)
	require := require.New(t)
	c, err := NewRequestClient(httpUrl, true, fakeApiKey)
	require.NoError(err, "NewClient should not return an error")
	require.NotNil(c, "NewClient should return a client")
	require.Equal(u, c.URL.String(), "NewClient should set the correct URL")
	require.Equal(fakeApiKey, c.apiKey)
	require.False(c.httpClient.Transport.(*ClientTransport).inner.(*http.Transport).TLSClientConfig.InsecureSkipVerify)
}

// Need tests for BasicAuth
func TestDoPostRequest(t *testing.T) {
	type testPayload struct {
		Testing string `json:"testing"`
	}
	expectedOutput := testPayload{
		Testing: "moomoo",
	}
	expectedApiKeyHeader := "x-api-key"
	parameters := []struct {
		name            string
		endpoint        string
		apiKey          string
		expectedURL     string
		expectedPayload testPayload
	}{
		{
			name:            "withoutHeader",
			endpoint:        "test",
			apiKey:          "",
			expectedURL:     "/test",
			expectedPayload: expectedOutput,
		},
		{
			name:            "withHeader",
			endpoint:        "test2",
			apiKey:          "testKey",
			expectedURL:     "/test2",
			expectedPayload: expectedOutput,
		},
	}
	for _, param := range parameters {
		t.Run(param.name, func(t *testing.T) {
			require := require.New(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(param.expectedURL, r.URL.String(), "DoPostRequest should use the correct URL")
				if param.apiKey != "" {
					require.Equal(param.apiKey, r.Header.Get(expectedApiKeyHeader), "DoPostRequest should have the correct api key")
				} else {
					require.Empty(r.Header.Get(expectedApiKeyHeader), "DoPostRequest should not have an api key")
				}
				body := testPayload{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				require.Equal(body, param.expectedPayload, "DoPostRequest should have the correct body")
				w.Write([]byte(`{"testing":"moomoo"}`))
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			target := testPayload{}

			httpUrl, _ := url.Parse(ts.URL)
			client, err := NewRequestClient(httpUrl, false, param.apiKey)
			if err != nil {
				panic(err)
			}
			require.Nil(err, "NewClient should not return an error")
			require.NotNil(client, "NewClient should return a client")
			outputBytes, marshErr := json.Marshal(expectedOutput)
			require.Nil(marshErr, "json.Marshal should not return an error")
			err = client.DoPostRequest(param.endpoint, &target, outputBytes)
			require.Nil(err, "DoPostRequest should not return an error: %s", err)
			require.Equal(expectedOutput, target, "DoPostRequest should return the correct data")
		})
	}
}

func TestDoRequest(t *testing.T) {
	type testPayload struct {
		Testing string `json:"testing"`
	}
	expectedOutput := testPayload{
		Testing: "coocoo",
	}

	expectedApiKeyHeader := "x-api-key"

	parameters := []struct {
		name        string
		apiKey      string
		endpoint    string
		expectedURL string
	}{
		{
			name:        "withNothing",
			apiKey:      "",
			endpoint:    "test",
			expectedURL: "/test",
		},
		{
			name:        "withHeaders",
			apiKey:      "testKey",
			endpoint:    "test2",
			expectedURL: "/test2",
		},
	}
	for _, param := range parameters {
		t.Run(param.name, func(t *testing.T) {
			require := require.New(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(param.expectedURL, r.URL.String(), "DoRequest should use the correct URL")
				if param.apiKey != "" {
					require.Equal(param.apiKey, r.Header.Get(expectedApiKeyHeader), "DoRequest should have the correct api key")
				} else {
					require.Empty(r.Header.Get(expectedApiKeyHeader), "DoRequest should not have an api key")
				}
				w.Write([]byte(`{"testing":"coocoo"}`))
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			target := testPayload{}

			httpUrl, urlErr := url.Parse(ts.URL)
			require.Nil(urlErr, "url.Parse should not return an error")
			client, err := NewRequestClient(httpUrl, false, param.apiKey)
			if err != nil {
				panic(err)
			}
			require.Nil(err, "NewClient should not return an error")
			require.NotNil(client, "NewClient should return a client")
			err = client.DoRequest(param.endpoint, &target)
			require.Nil(err, "DoPostRequest should not return an error: %s", err)
			require.Equal(expectedOutput, target, "DoPostRequest should return the correct data")
		})
	}
}

func TestDoRequest_PanicRecovery(t *testing.T) {
	require := require.New(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ret := struct {
			TestField  string
			TestField2 string
		}{
			TestField:  "moomoo",
			TestField2: "coocoo",
		}
		s, err := json.Marshal(ret)
		require.NoError(err)
		w.Write(s)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	httpUrl, _ := url.Parse(ts.URL)
	client, err := NewRequestClient(httpUrl, false, "")
	require.Nil(err, "NewClient should not return an error")
	require.NotNil(client, "NewClient should return a client")

	err = client.DoRequest("test", nil)
	// should panic since `DoRequest` will attempt to unmarshal into nil target
	// `panic: json: Unmarshal(nil)`
	require.NotPanics(func() {
		require.Error(err, "DoRequest should return an error: %s", err)
	}, "DoRequest should recover from a panic")
}
