package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIndexHandler verifies the landing page links to the configured metrics
// path (not a hardcoded '/metrics') and html-escapes it before interpolation.
func TestIndexHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		metricsPath string
		wantLink    string
	}{
		{"default path", "/metrics", `href='/metrics'`},
		{"custom path", "/custom-metrics", `href='/custom-metrics'`},
		{"path with html-significant char is escaped", "/a'b", `href='/a&#39;b'`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			IndexHandler(tc.metricsPath).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.wantLink) {
				t.Errorf("body = %q, want it to contain %q", body, tc.wantLink)
			}
		})
	}
}
