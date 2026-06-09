package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// newEngine wires a gin engine the same way internal/server/server.go does.
func newEngine(reg *prometheus.Registry, tdarrInstance string) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Route Not Found: Try /metrics"})
	})
	router.Use(RequestLogger())
	router.GET("/metrics", MetricsHandler(reg, promhttp.HandlerOpts{}, tdarrInstance))
	router.GET("/", IndexHandler())
	router.GET("/healthz", HealthzHandler())
	return router
}

func TestStaticHandlers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		method       string
		path         string
		wantStatus   int
		wantContains string
	}{
		{
			name:         "healthz returns ok json",
			method:       http.MethodGet,
			path:         "/healthz",
			wantStatus:   http.StatusOK,
			wantContains: `{"status":"ok"}`,
		},
		{
			name:         "index returns html body",
			method:       http.MethodGet,
			path:         "/",
			wantStatus:   http.StatusOK,
			wantContains: "<h1>tdarr-exporter</h1>",
		},
		{
			name:         "unknown route returns 404 json",
			method:       http.MethodGet,
			path:         "/does-not-exist",
			wantStatus:   http.StatusNotFound,
			wantContains: `"error":"Route Not Found: Try /metrics"`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine := newEngine(prometheus.NewRegistry(), "test-instance")
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)

			engine.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tc.wantContains) {
				t.Fatalf("body = %q, want it to contain %q", rec.Body.String(), tc.wantContains)
			}
		})
	}
}

func TestMetricsHandler(t *testing.T) {
	t.Parallel()

	const instance = "metrics-instance"
	engine := newEngine(prometheus.NewRegistry(), instance)

	doGet := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		engine.ServeHTTP(rec, req)
		return rec
	}

	// First scrape. The scrape_requests_total counter is incremented in a
	// deferred func that runs *after* promhttp has already written the body,
	// so it is not visible until a later scrape. The scrape_duration gauge is
	// registered eagerly, so it (and the const label) is present immediately.
	rec := doGet()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q, want Prometheus text format (text/plain...)", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"tdarr_scrape_duration_seconds",
		`tdarr_instance="` + instance + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\nbody:\n%s", want, body)
		}
	}

	// Second scrape: now the counter from the first request is exposed.
	body2 := doGet().Body.String()
	if !strings.Contains(body2, "tdarr_scrape_requests_total") {
		t.Fatalf("metrics body missing %q\nbody:\n%s", "tdarr_scrape_requests_total", body2)
	}
	if !strings.Contains(body2, `tdarr_instance="`+instance+`"`) {
		t.Fatalf("counter missing const label tdarr_instance=%q\nbody:\n%s", instance, body2)
	}

	// Third scrape: counter must have incremented relative to the second.
	body3 := doGet().Body.String()
	got2 := counterValue(t, body2, instance)
	got3 := counterValue(t, body3, instance)
	if got2 < 0 || got3 < 0 {
		t.Fatalf("scrape_requests_total{code=\"200\"} not found: second=%v third=%v", got2, got3)
	}
	if got3 <= got2 {
		t.Fatalf("scrape_requests_total did not increment: second=%v third=%v", got2, got3)
	}
}

// counterValue extracts the tdarr_scrape_requests_total{code="200"} sample
// value from a Prometheus text exposition body. Returns -1 if not present.
func counterValue(t *testing.T, body, instance string) float64 {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "tdarr_scrape_requests_total{") &&
			strings.Contains(line, `code="200"`) &&
			strings.Contains(line, `tdarr_instance="`+instance+`"`) {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				t.Fatalf("malformed counter line: %q", line)
			}
			var v float64
			if _, err := fmt.Sscan(fields[len(fields)-1], &v); err != nil {
				t.Fatalf("parse counter value %q: %v", fields[len(fields)-1], err)
			}
			return v
		}
	}
	return -1
}

func TestRequestLoggerPassesThrough(t *testing.T) {
	t.Parallel()

	const wantBody = "logger-passthrough-body"
	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/probe", func(c *gin.Context) {
		c.String(http.StatusTeapot, wantBody)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d (RequestLogger altered status)", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != wantBody {
		t.Fatalf("body = %q, want %q (RequestLogger altered body)", rec.Body.String(), wantBody)
	}
}

// TestMetricsHandler_PromhttpInstrumented verifies P2.3: the standard
// promhttp_metric_handler_* series are emitted and carry the tdarr_instance label
// (injected via WrapRegistererWith). requests_total is incremented in a deferred
// path after the body is written, so it is visible only on a later scrape.
func TestMetricsHandler_PromhttpInstrumented(t *testing.T) {
	t.Parallel()

	const instance = "promhttp-instance"
	engine := newEngine(prometheus.NewRegistry(), instance)

	doGet := func() string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		engine.ServeHTTP(rec, req)
		return rec.Body.String()
	}

	doGet()         // first scrape arms the deferred requests_total increment
	body := doGet() // second scrape exposes it

	// All three standard handler series must be present. requests_total + in_flight come
	// from InstrumentMetricHandler; errors_total is registered only because opts.Registry is
	// set (the A1 fix) and is pre-seeded to 0, so it appears on every scrape.
	for _, name := range []string{
		"promhttp_metric_handler_requests_total",
		"promhttp_metric_handler_requests_in_flight",
		"promhttp_metric_handler_errors_total",
	} {
		if !strings.Contains(body, name+"{") {
			t.Fatalf("missing %s series\nbody:\n%s", name, body)
		}
	}
	// The wrap is the point of P2.3: every handler-metric sample must carry tdarr_instance.
	// Checking errors_total specifically locks the A1 fix — it is labeled only when
	// opts.Registry points at the WRAPPED registry, not the raw one.
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "promhttp_metric_handler_") &&
			!strings.Contains(line, `tdarr_instance="`+instance+`"`) {
			t.Fatalf("promhttp handler metric missing tdarr_instance label:\n%s", line)
		}
	}
}
