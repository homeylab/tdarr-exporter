package client

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

// TestUnmarshalBody exercises unmarshalBody's two branches: a successful decode
// and a malformed-JSON decode error.
func TestUnmarshalBody(t *testing.T) {
	t.Parallel()

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
			errSubstr: "failed to decode response body",
		},
	}

	for _, tt := range tests {
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

// TestUnmarshalBody_LogsBodyHeadOnDecodeError verifies the decode-failure
// diagnostic: a non-JSON body (e.g. an HTML error page from a proxy/auth failure)
// is debug-logged as body_head so operators can tell it apart from schema drift.
func TestUnmarshalBody_LogsBodyHeadOnDecodeError(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	c := &RequestClient{logger: zerolog.New(&logBuf).Level(zerolog.DebugLevel)}

	var target struct {
		Name string `json:"name"`
	}
	body := "<html><body>401 Unauthorized</body></html>"
	err := c.unmarshalBody(strings.NewReader(body), &target)
	if err == nil {
		t.Fatalf("unmarshalBody: expected decode error for non-JSON body, got nil")
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "failed to decode response body") {
		t.Fatalf("expected debug log of decode failure, got %q", logged)
	}
	// The head of the offending body must be captured for troubleshooting.
	if !strings.Contains(logged, "401 Unauthorized") {
		t.Fatalf("expected body head in log, got %q", logged)
	}
}

// TestUnmarshalBody_NoDebugOutputAtInfoLevel verifies that at info level a decode
// failure still returns the error but emits no debug output (no body_head line).
// The tee-capture is skipped in this case too, but that is a non-observable perf
// detail; this asserts only the visible behavior.
func TestUnmarshalBody_NoDebugOutputAtInfoLevel(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	c := &RequestClient{logger: zerolog.New(&logBuf).Level(zerolog.InfoLevel)}

	var target struct {
		Name string `json:"name"`
	}
	err := c.unmarshalBody(strings.NewReader("<html>401 Unauthorized</html>"), &target)
	if err == nil {
		t.Fatalf("unmarshalBody: expected decode error, got nil")
	}
	if logged := logBuf.String(); logged != "" {
		t.Fatalf("expected no debug output at info level, got %q", logged)
	}
}

// TestCappedWriter verifies the writer keeps only the first limit bytes yet always
// reports a full-length write (so an io.TeeReader driving it never short-writes).
func TestCappedWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := &cappedWriter{buf: &buf, limit: 4}

	n, err := w.Write([]byte("abcdef"))
	if err != nil {
		t.Fatalf("Write: unexpected error: %v", err)
	}
	if n != 6 {
		t.Fatalf("Write returned n=%d, want 6 (full input length)", n)
	}
	if got := buf.String(); got != "abcd" {
		t.Fatalf("buffered %q, want %q", got, "abcd")
	}

	// A second write past the limit is fully discarded but still reports its length.
	n, err = w.Write([]byte("ghi"))
	if err != nil {
		t.Fatalf("Write: unexpected error: %v", err)
	}
	if n != 3 {
		t.Fatalf("Write returned n=%d, want 3", n)
	}
	if got := buf.String(); got != "abcd" {
		t.Fatalf("buffered %q after over-limit write, want %q", got, "abcd")
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
