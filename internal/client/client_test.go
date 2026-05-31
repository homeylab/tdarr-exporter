package client

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

// panicReader is an io.Reader whose Read method panics, used to drive the
// defer/recover branch in unmarshalBody.
type panicReader struct{}

func (panicReader) Read([]byte) (int, error) {
	panic("boom from Read")
}

// TestUnmarshalBody exercises unmarshalBody's three branches: a successful
// decode, a malformed-JSON decode error, and the panic-recover path where the
// body's Read panics and must be converted into an error rather than crashing.
func TestUnmarshalBody(t *testing.T) {
	t.Parallel()

	// Explicit logger so the test does not depend on ambient log level. Nop()
	// keeps the debug-only body-copy branch (which would re-read the panicking
	// body) disabled; the recover-into-error behavior is what this test asserts.
	c := &RequestClient{logger: zerolog.Nop()}

	tests := []struct {
		name      string
		body      io.Reader
		wantErr   bool
		errSubstr string // checked only when wantErr is true
		wantField string // checked only on success
	}{
		{
			name:      "valid json decodes into target",
			body:      strings.NewReader(`{"name":"tdarr"}`),
			wantErr:   false,
			wantField: "tdarr",
		},
		{
			name:      "malformed json returns error",
			body:      strings.NewReader(`{not-json`),
			wantErr:   true,
			errSubstr: "", // any decode error is acceptable
		},
		{
			name:      "panicking reader is recovered into an error",
			body:      panicReader{},
			wantErr:   true,
			errSubstr: "recovered",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var target struct {
				Name string `json:"name"`
			}
			err := c.unmarshalBody(tt.body, &target)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("unmarshalBody: expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("unmarshalBody error = %q, want substring %q", err.Error(), tt.errSubstr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unmarshalBody: unexpected error: %v", err)
			}
			if target.Name != tt.wantField {
				t.Fatalf("decoded Name = %q, want %q", target.Name, tt.wantField)
			}
		})
	}
}

// TestNewRequestClient_DoesNotLeakInsecureIntoDefaultTransport proves that
// constructing a RequestClient with verifySsl=false (InsecureSkipVerify=true) does
// NOT leak that insecure setting into the process-global http.DefaultTransport.
// This is the regression test for the global-transport mutation bug fixed in Task 4:
// the old code assigned a {InsecureSkipVerify: true} config directly onto
// http.DefaultTransport, silently disabling TLS verification process-wide.
//
// Note: we assert on InsecureSkipVerify specifically rather than object identity,
// because http.Transport.Clone() itself lazily initializes DefaultTransport's
// TLSClientConfig with HTTP/2 defaults (InsecureSkipVerify=false). That is benign;
// the bug we guard against is our InsecureSkipVerify=true leaking out.
func TestNewRequestClient_DoesNotLeakInsecureIntoDefaultTransport(t *testing.T) {
	t.Parallel()

	u, _ := url.Parse("http://example.com")
	// verifySsl=false means InsecureSkipVerify=true in the new client — the most
	// aggressive change; old code would set the global to InsecureSkipVerify=true here.
	_, err := NewRequestClient(u, false, 15, "")
	if err != nil {
		t.Fatalf("NewRequestClient: %v", err)
	}

	// The global transport must never end up with our insecure setting.
	if cfg := http.DefaultTransport.(*http.Transport).TLSClientConfig; cfg != nil && cfg.InsecureSkipVerify {
		t.Errorf("http.DefaultTransport leaked InsecureSkipVerify=true; the global transport was mutated")
	}
}

// TestNewRequestClient_ConcurrentConstruction verifies that constructing many
// RequestClients concurrently does not trigger a data race on http.DefaultTransport.
// Run with: go test -race ./internal/client/
func TestNewRequestClient_ConcurrentConstruction(t *testing.T) {
	t.Parallel()

	u, _ := url.Parse("http://example.com")
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			verifySsl := i%2 == 0 // alternate true/false for variety
			_, err := NewRequestClient(u, verifySsl, 15, "test-key")
			if err != nil {
				// t.Error is goroutine-safe; t.Fatal is not.
				t.Error("NewRequestClient concurrent construction error:", err)
			}
		}()
	}
	wg.Wait()
}
