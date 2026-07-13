package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// payloadTarget is a small struct the request methods unmarshal JSON into.
type payloadTarget struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// newTestClient builds a RequestClient pointed at the given server URL with a
// short timeout, TLS verification disabled, and the provided api key.
func newTestClient(t *testing.T, serverURL, apiKey string) *RequestClient {
	t.Helper()
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", serverURL, err)
	}
	return NewRequestClient(u, false, 5, apiKey)
}

// TestDoRequest_HappyPath verifies a GET to a path: the server sees method GET
// and the JoinPath'd request path, and the JSON response unmarshals into target.
func TestDoRequest_HappyPath(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"tdarr","count":7}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "secret-key")

	var target payloadTarget
	if err := c.DoRequest(context.Background(), "/api/v2/status", &target); err != nil {
		t.Fatalf("DoRequest: unexpected error: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method: want %s, got %s", http.MethodGet, gotMethod)
	}
	if gotPath != "/api/v2/status" {
		t.Errorf("path: want %q, got %q", "/api/v2/status", gotPath)
	}
	if target.Name != "tdarr" || target.Count != 7 {
		t.Errorf("target: want {tdarr 7}, got %+v", target)
	}
}

// TestDoRequest_QueryParams verifies that multiple QueryParams keys/values are
// sent on the request URL, merged with any query already present on the base URL.
func TestDoRequest_QueryParams(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	// Base URL carries an existing query param that must be preserved.
	c := newTestClient(t, srv.URL+"?base=preserved", "secret-key")

	params := QueryParams{
		"table":  []string{"library"},
		"fields": []string{"id", "name"},
	}
	var target payloadTarget
	if err := c.DoRequest(context.Background(), "/api/v2/list", &target, params); err != nil {
		t.Fatalf("DoRequest: unexpected error: %v", err)
	}

	if got := gotQuery.Get("base"); got != "preserved" {
		t.Errorf("query base: want %q, got %q", "preserved", got)
	}
	if got := gotQuery.Get("table"); got != "library" {
		t.Errorf("query table: want %q, got %q", "library", got)
	}
	gotFields := gotQuery["fields"]
	if len(gotFields) != 2 || gotFields[0] != "id" || gotFields[1] != "name" {
		t.Errorf("query fields: want [id name], got %v", gotFields)
	}
}

// TestDoRequest_ApiKeyHeader verifies the x-api-key header is present when a key
// is configured and absent when the key is empty.
func TestDoRequest_ApiKeyHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		apiKey      string
		wantPresent bool
	}{
		{name: "key set sends header", apiKey: "secret-key", wantPresent: true},
		{name: "empty key omits header", apiKey: "", wantPresent: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotHeader string
			var headerPresent bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHeader = r.Header.Get("x-api-key")
				_, headerPresent = r.Header["X-Api-Key"]
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{}`)
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL, tt.apiKey)

			var target payloadTarget
			if err := c.DoRequest(context.Background(), "/api/v2/status", &target); err != nil {
				t.Fatalf("DoRequest: unexpected error: %v", err)
			}

			if tt.wantPresent {
				if !headerPresent {
					t.Fatalf("x-api-key header: want present, got absent")
				}
				if gotHeader != tt.apiKey {
					t.Errorf("x-api-key: want %q, got %q", tt.apiKey, gotHeader)
				}
			} else if headerPresent {
				t.Errorf("x-api-key header: want absent, got %q", gotHeader)
			}
		})
	}
}

// TestDoRequest_UnmarshalError verifies that invalid JSON from the server
// surfaces as a non-nil error from DoRequest.
func TestDoRequest_UnmarshalError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{not-valid-json`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "secret-key")

	var target payloadTarget
	if err := c.DoRequest(context.Background(), "/api/v2/status", &target); err == nil {
		t.Fatal("DoRequest: want error for invalid JSON, got nil")
	}
}

// TestDoPostRequest_HappyPath verifies a POST: method POST, JSON content type,
// the x-api-key header, the request body equals the payload, and the response
// unmarshals into target.
func TestDoPostRequest_HappyPath(t *testing.T) {
	t.Parallel()

	var (
		gotMethod      string
		gotContentType string
		gotApiKey      string
		gotBody        string
		gotPath        string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotApiKey = r.Header.Get("x-api-key")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"posted","count":3}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "secret-key")

	payload := []byte(`{"query":"value"}`)
	var target payloadTarget
	if err := c.DoPostRequest(context.Background(), "/api/v2/cruddb", &target, payload); err != nil {
		t.Fatalf("DoPostRequest: unexpected error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method: want %s, got %s", http.MethodPost, gotMethod)
	}
	if gotPath != "/api/v2/cruddb" {
		t.Errorf("path: want %q, got %q", "/api/v2/cruddb", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type: want application/json, got %q", gotContentType)
	}
	if gotApiKey != "secret-key" {
		t.Errorf("x-api-key: want secret-key, got %q", gotApiKey)
	}
	if gotBody != string(payload) {
		t.Errorf("body: want %q, got %q", string(payload), gotBody)
	}
	if target.Name != "posted" || target.Count != 3 {
		t.Errorf("target: want {posted 3}, got %+v", target)
	}
}
