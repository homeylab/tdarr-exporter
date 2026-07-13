package server

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// freePort returns an available TCP port on 127.0.0.1 by binding to :0 and
// immediately releasing it. There is a small race window before ServeHttp
// rebinds, which is acceptable for a test on the loopback interface.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host/port: %v", err)
	}
	return port
}

// waitForServer polls the address with net.Dial until it accepts a connection
// or the deadline passes. No time.Sleep loop over a fixed count.
func waitForServer(t *testing.T, addr string, deadline time.Duration) {
	t.Helper()
	stop := time.Now().Add(deadline)
	for time.Now().Before(stop) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become reachable within %s", addr, deadline)
}

// TestListenAddressJoinHostPort pins the contract ServeHttp relies on when it
// builds http.Server.Addr with net.JoinHostPort: the result is accepted by
// net.Listen for IPv4, IPv6, and the common defaults. It documents why the
// naive fmt.Sprintf("%s:%s", host, port) is wrong — that form yields an
// unparseable "too many colons" address for IPv6 hosts like "::".
// (ServeHttp's own use of the address is exercised by TestServeHttpLifecycle.)
func TestListenAddressJoinHostPort(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		wantAddr string
	}{
		{name: "ipv4 loopback", host: "127.0.0.1", wantAddr: "127.0.0.1:0"},
		{name: "ipv4 unspecified", host: "0.0.0.0", wantAddr: "0.0.0.0:0"},
		{name: "ipv6 unspecified", host: "::", wantAddr: "[::]:0"},
		{name: "ipv6 loopback", host: "::1", wantAddr: "[::1]:0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := net.JoinHostPort(tt.host, "0")
			if addr != tt.wantAddr {
				t.Fatalf("net.JoinHostPort(%q, \"0\") = %q, want %q", tt.host, addr, tt.wantAddr)
			}

			ln, err := net.Listen("tcp", addr)
			if err != nil {
				if strings.Contains(tt.host, ":") {
					t.Skipf("skipping IPv6 bind: environment appears to lack IPv6 support: %v", err)
				}
				t.Fatalf("net.Listen(%q) failed: %v", addr, err)
			}
			defer func() { _ = ln.Close() }()
		})
	}
}

func TestServeHttpLifecycle(t *testing.T) {
	port := freePort(t)
	addr := net.JoinHostPort("127.0.0.1", port)

	wg := &sync.WaitGroup{}
	stopChan := make(chan bool)
	errChan := make(chan error, 1)
	cfg := HttpServerConfig{
		TdarrInstance:   "lifecycle",
		ListenAddress:   "127.0.0.1",
		PrometheusPort:  port,
		PrometheusPath:  "/metrics",
		GracefulTimeout: 5 * time.Second,
	}

	wg.Add(1)
	go ServeHttp(wg, prometheus.NewRegistry(), cfg, stopChan, errChan)

	waitForServer(t, addr, 2*time.Second)

	// Confirm it actually serves a route.
	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// No spurious error should have been delivered while serving.
	select {
	case err := <-errChan:
		t.Fatalf("unexpected error during normal serving: %v", err)
	default:
	}

	// Trigger graceful shutdown and assert the WaitGroup completes (i.e.
	// ServeHttp returned via wg.Done, not os.Exit).
	stopChan <- true

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeHttp did not return / WaitGroup did not complete after shutdown")
	}

	// A clean shutdown must not deliver an error.
	select {
	case err := <-errChan:
		t.Fatalf("unexpected error on clean shutdown: %v", err)
	default:
	}
}

func TestServeHttpListenErrorDeliveredOnChannel(t *testing.T) {
	// Occupy a port so ListenAndServe fails with "address already in use"
	// instead of crashing the process via log.Fatal/os.Exit.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to occupy port: %v", err)
	}
	defer func() { _ = occupied.Close() }()
	_, port, err := net.SplitHostPort(occupied.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host/port: %v", err)
	}

	wg := &sync.WaitGroup{}
	stopChan := make(chan bool, 1)
	errChan := make(chan error, 1)
	cfg := HttpServerConfig{
		TdarrInstance:   "listen-error",
		ListenAddress:   "127.0.0.1",
		PrometheusPort:  port,
		PrometheusPath:  "/metrics",
		GracefulTimeout: 1 * time.Second,
	}

	wg.Add(1)
	go ServeHttp(wg, prometheus.NewRegistry(), cfg, stopChan, errChan)

	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("expected a non-nil ListenAndServe error on errChan")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ListenAndServe error was not delivered on errChan")
	}

	// ServeHttp is still blocked on stopChan after the listen error (matching
	// production wiring where main sends stop after receiving the error).
	// Release it and confirm clean return.
	stopChan <- true

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ServeHttp did not return after stop following listen error")
	}
}

// blockingCollector parks Collect until release is closed, holding a /metrics
// request active so srv.Shutdown cannot finish before its deadline. entered
// signals that the scrape (and thus the request) is in flight.
type blockingCollector struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingCollector) Describe(chan<- *prometheus.Desc) {}
func (b *blockingCollector) Collect(chan<- prometheus.Metric) {
	close(b.entered)
	<-b.release
}

// TestServeHttpShutdownErrorSecondSendDoesNotBlock pins the errChan capacity
// contract: with one slot per sender (cap 2, matching main's wiring), ServeHttp
// must still return when a Shutdown error is sent while an earlier error
// already sits undrained in the channel. With cap 1 the second send would
// block forever and the WaitGroup would never complete.
func TestServeHttpShutdownErrorSecondSendDoesNotBlock(t *testing.T) {
	port := freePort(t)
	addr := net.JoinHostPort("127.0.0.1", port)

	wg := &sync.WaitGroup{}
	stopChan := make(chan bool)
	// Mirror production wiring (cmd/exporter/main.go) and pre-fill one slot to
	// simulate an earlier undrained send.
	errChan := make(chan error, 2)
	errChan <- fmt.Errorf("simulated earlier undrained error")

	bc := &blockingCollector{entered: make(chan struct{}), release: make(chan struct{})}
	registry := prometheus.NewRegistry()
	registry.MustRegister(bc)

	cfg := HttpServerConfig{
		TdarrInstance:  "double-error",
		ListenAddress:  "127.0.0.1",
		PrometheusPort: port,
		PrometheusPath: "/metrics",
		// Short deadline so Shutdown gives up on the parked request quickly.
		GracefulTimeout: 100 * time.Millisecond,
	}

	wg.Add(1)
	go ServeHttp(wg, registry, cfg, stopChan, errChan)
	waitForServer(t, addr, 2*time.Second)

	// Park a scrape inside the handler so the connection stays active across
	// Shutdown. Release it at the end so the client goroutine can exit.
	scrapeDone := make(chan struct{})
	go func() {
		defer close(scrapeDone)
		resp, err := http.Get(fmt.Sprintf("http://%s/metrics", addr))
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-bc.entered

	stopChan <- true

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeHttp did not return: Shutdown-error send blocked on an undrained errChan")
	}

	// Both errors must be present: the pre-filled one and the Shutdown
	// deadline error.
	<-errChan
	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("expected a non-nil Shutdown error as the second channel entry")
		}
	default:
		t.Fatal("Shutdown error was not delivered as the second channel entry")
	}

	close(bc.release)
	<-scrapeDone
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
			mux := newMux(HttpServerConfig{TdarrInstance: "test-instance", PrometheusPath: "/metrics"}, prometheus.NewRegistry())
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

// TestCustomPrometheusPathReflectedInRoutes verifies that when prometheus_path
// is customized, both the landing-page link and the 404 fallback hint point at
// the configured path rather than a hardcoded '/metrics'.
func TestCustomPrometheusPathReflectedInRoutes(t *testing.T) {
	t.Parallel()

	const customPath = "/custom-metrics"
	mux := newMux(HttpServerConfig{TdarrInstance: "test-instance", PrometheusPath: customPath}, prometheus.NewRegistry())

	tests := []struct {
		name         string
		path         string
		wantStatus   int
		wantContains string
	}{
		{"landing page links to custom path", "/", http.StatusOK, `href='/custom-metrics'`},
		{"404 hint names custom path", "/does-not-exist", http.StatusNotFound, `"error":"Route Not Found: Try /custom-metrics"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)

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
	mux := newMux(HttpServerConfig{TdarrInstance: instance, PrometheusPath: "/metrics"}, prometheus.NewRegistry())

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

// TestMetricsHandler_PromhttpInstrumented verifies P2.3: the standard
// promhttp_metric_handler_* series are emitted and carry the tdarr_instance label
// (injected via WrapRegistererWith). requests_total is incremented in a deferred
// path after the body is written, so it is visible only on a later scrape.
func TestMetricsHandler_PromhttpInstrumented(t *testing.T) {
	t.Parallel()

	const instance = "promhttp-instance"
	mux := newMux(HttpServerConfig{TdarrInstance: instance, PrometheusPath: "/metrics"}, prometheus.NewRegistry())

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
	mux := newMux(HttpServerConfig{TdarrInstance: "continue-on-error", PrometheusPath: "/metrics"}, reg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (ContinueOnError must not 500 on a Gather error)", rec.Code)
	}
}
