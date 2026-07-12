package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// newMux wires a handler stack the same way internal/server/server.go does.
func newMux(reg *prometheus.Registry, tdarrInstance string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", MetricsHandler(reg, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError}, tdarrInstance))
	mux.Handle("GET /{$}", IndexHandler())
	mux.Handle("GET /healthz", HealthzHandler())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Route Not Found: Try /metrics"})
	})
	return Recovery(RequestLogger(mux))
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
		{
			// A wrong-method request to a known path is absorbed by the
			// catch-all `/` handler (ServeMux routes it there rather than
			// returning 405, because a less-specific matching pattern exists).
			// This matches gin's old NoRoute 404 — no behavior delta.
			name:         "post to healthz returns 404 via catch-all",
			method:       http.MethodPost,
			path:         "/healthz",
			wantStatus:   http.StatusNotFound,
			wantContains: `"error":"Route Not Found: Try /metrics"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := newMux(prometheus.NewRegistry(), "test-instance")
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)

			mux.ServeHTTP(rec, req)

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
	mux := newMux(prometheus.NewRegistry(), instance)

	doGet := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		mux.ServeHTTP(rec, req)
		return rec
	}

	// First scrape. The promhttp_metric_handler_requests_total counter is incremented
	// inside promhttp's InstrumentMetricHandler after the body is written, so its
	// count is not visible until a later scrape. But the handler series are all
	// registered eagerly and pre-seeded to 0 — requests_total/requests_in_flight by
	// InstrumentMetricHandler, errors_total because opts.Registry is set — so they
	// (and the const label) are present immediately.
	rec := doGet()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q, want Prometheus text format (text/plain...)", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `tdarr_instance="`+instance+`"`) {
		t.Fatalf("metrics body missing %q\nbody:\n%s", `tdarr_instance="`+instance+`"`, body)
	}

	// Second scrape: now the counter from the first request is exposed.
	body2 := doGet().Body.String()
	if !strings.Contains(body2, "promhttp_metric_handler_requests_total") {
		t.Fatalf("metrics body missing %q\nbody:\n%s", "promhttp_metric_handler_requests_total", body2)
	}
	if !strings.Contains(body2, `tdarr_instance="`+instance+`"`) {
		t.Fatalf("counter missing const label tdarr_instance=%q\nbody:\n%s", instance, body2)
	}

	// Third scrape: counter must have incremented relative to the second.
	body3 := doGet().Body.String()
	got2 := counterValue(t, body2, instance)
	got3 := counterValue(t, body3, instance)
	if got2 < 0 || got3 < 0 {
		t.Fatalf("promhttp_metric_handler_requests_total{code=\"200\"} not found: second=%v third=%v", got2, got3)
	}
	if got3 <= got2 {
		t.Fatalf("promhttp_metric_handler_requests_total did not increment: second=%v third=%v", got2, got3)
	}
}

// counterValue extracts the promhttp_metric_handler_requests_total{code="200"} sample
// value from a Prometheus text exposition body. Returns -1 if not present.
func counterValue(t *testing.T, body, instance string) float64 {
	t.Helper()
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "promhttp_metric_handler_requests_total{") &&
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
	h := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(wantBody))
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	h.ServeHTTP(rec, req)

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
	mux := newMux(prometheus.NewRegistry(), instance)

	doGet := func() string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		mux.ServeHTTP(rec, req)
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
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "promhttp_metric_handler_") &&
			!strings.Contains(line, `tdarr_instance="`+instance+`"`) {
			t.Fatalf("promhttp handler metric missing tdarr_instance label:\n%s", line)
		}
	}
}

// dupCollector emits the same metric twice, which makes Registry.Gather return a
// "collected before with the same name and label values" error — a deterministic way
// to drive the handler's gather-error path without a panic.
type dupCollector struct{ desc *prometheus.Desc }

func (d dupCollector) Describe(ch chan<- *prometheus.Desc) { ch <- d.desc }
func (d dupCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(d.desc, prometheus.GaugeValue, 1)
	ch <- prometheus.MustNewConstMetric(d.desc, prometheus.GaugeValue, 1)
}

// TestMetricsHandler_ContinueOnError_Serves200OnGatherError locks the ContinueOnError
// wiring (matches production server.go): on a registry-level Gather error the handler
// must still serve HTTP 200 with whatever could be gathered, NOT 500. Under the default
// HTTPErrorOnError this would be 500, so the test fails if newMux's opts regress.
func TestMetricsHandler_ContinueOnError_Serves200OnGatherError(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	reg.MustRegister(dupCollector{desc: prometheus.NewDesc("dup_metric", "duplicate on purpose", nil, nil)})
	mux := newMux(reg, "continue-on-error")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (ContinueOnError must not 500 on a Gather error)", rec.Code)
	}
}

// TestRecoveryConvertsPanicTo500 verifies the Recovery middleware converts a
// handler panic into a 500 response instead of crashing the connection
// (replaces gin.Recovery's equivalent behavior).
func TestRecoveryConvertsPanicTo500(t *testing.T) {
	t.Parallel()

	h := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500, got %d", rec.Code)
	}
}
