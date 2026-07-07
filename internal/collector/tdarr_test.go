package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
)

// newTestConfig builds a Config with sane test defaults. UrlParsed is set so the
// config is well-formed; the injected fakeTdarrAPI never makes a network call.
// HttpMaxConcurrency=1 keeps pie-fetch ordering deterministic and race-free.
func newTestConfig(t *testing.T) config.Config {
	t.Helper()
	u, err := url.Parse("http://tdarr.test")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return config.Config{
		UrlParsed:          u,
		InstanceName:       "test-instance",
		ApiKey:             "test-key",
		VerifySsl:          false,
		HttpTimeoutSeconds: 5,
		TdarrStatsPath:     "/api/v2/cruddb",
		TdarrPieStatsPath:  "/api/v2/stats/get-pies",
		TdarrNodePath:      "/api/v2/get-nodes",
		TdarrStatusPath:    "/api/v2/status",
		HttpMaxConcurrency: 1,
	}
}

// gatherMetricFamilies registers the collector with a fresh registry and returns the gathered metric families.
func gatherMetricFamilies(t *testing.T, c *TdarrCollector) []*dto.MetricFamily {
	t.Helper()
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	return mfs
}

// hasMetricFamily returns true if the named metric family appears in mfs with at least one sample.
func hasMetricFamily(mfs []*dto.MetricFamily, name string) bool {
	for _, mf := range mfs {
		if mf.GetName() == name && len(mf.GetMetric()) > 0 {
			return true
		}
	}
	return false
}

// upValueFromFamilies extracts tdarr_up gauge value from gathered families, -1 if absent.
func upValueFromFamilies(mfs []*dto.MetricFamily) float64 {
	for _, mf := range mfs {
		if mf.GetName() == "tdarr_up" {
			ms := mf.GetMetric()
			if len(ms) == 0 {
				return -1
			}
			return ms[0].GetGauge().GetValue()
		}
	}
	return -1
}

// getUpValue scrapes the registered collector and returns the value of tdarr_up.
// Returns -1.0 if the metric is absent.
func getUpValue(t *testing.T, c *TdarrCollector) float64 {
	t.Helper()
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "tdarr_up" {
			metrics := mf.GetMetric()
			if len(metrics) == 0 {
				t.Fatal("tdarr_up has no metric samples")
			}
			return metrics[0].GetGauge().GetValue()
		}
	}
	return -1.0
}

// validStatsBody returns a minimal valid TdarrMetric JSON body (stats-by-id response).
func validStatsBody() []byte {
	m := TdarrMetric{
		TotalFileCount:        10,
		TotalTranscodeCount:   2,
		TotalHealthCheckCount: 1,
		SizeDiff:              0,
		TdarrScore:            "0",
		HealthCheckScore:      "0",
	}
	b, _ := json.Marshal(m)
	return b
}

// validLibraryListBody returns a minimal valid []TdarrLibraryInfo JSON body.
func validLibraryListBody() []byte {
	libs := []TdarrLibraryInfo{
		{LibraryId: "lib1", Name: "Library One"},
	}
	b, _ := json.Marshal(libs)
	return b
}

// validPieBody returns a minimal valid TdarrPieStats JSON body.
func validPieBody() []byte {
	p := TdarrPieStats{
		PieStats: TdarrPieStat{
			TotalFiles:            10,
			TotalTranscodeCount:   2,
			TotalHealthCheckCount: 1,
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// validNodeBody returns a minimal valid map[string]TdarrNode JSON body (empty map = no nodes).
func validNodeBody() []byte {
	b, _ := json.Marshal(map[string]TdarrNode{})
	return b
}

// validStatusBody returns a valid /api/v2/status response body.
func validStatusBody() []byte {
	return []byte(`{"status":"good","isProduction":true,"os":"linux","version":"2.77.01","buildDate":"2026_05_29T12_20_24z","uptime":45}`)
}

// newSuccessFakeAPI builds a fakeTdarrAPI that responds successfully to every
// endpoint the collector calls, using the minimal valid bodies above. The single
// library "lib1" drives one get-pies call keyed on its libraryId.
func newSuccessFakeAPI(cfg config.Config) *fakeTdarrAPI {
	api := newFakeTdarrAPI()
	api.setResponse(fakeKey{path: cfg.TdarrStatsPath, disc: "StatisticsJSONDB"}, validStatsBody())
	api.setResponse(fakeKey{path: cfg.TdarrStatsPath, disc: "LibrarySettingsJSONDB"}, validLibraryListBody())
	api.setResponse(fakeKey{path: cfg.TdarrPieStatsPath, disc: "lib1"}, validPieBody())
	api.setResponse(fakeKey{path: cfg.TdarrNodePath}, validNodeBody())
	api.setResponse(fakeKey{path: cfg.TdarrStatusPath}, validStatusBody())
	return api
}

// TestCollect_ServerMetrics verifies the three /api/v2/status-derived series:
// uptime gauge, version/os info gauge, and the raw-label status info gauge.
func TestCollect_ServerMetrics(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, newSuccessFakeAPI(cfg))

	expected := `
# HELP tdarr_server_healthy 1 if Tdarr server self-reported status is healthy ("good"/"ok"/"healthy", case-insensitive), 0 otherwise. Raw status string is on tdarr_server_status_info.
# TYPE tdarr_server_healthy gauge
tdarr_server_healthy{tdarr_instance="test-instance"} 1
# HELP tdarr_server_info Tdarr server build metadata (value always 1); version and OS exposed as labels
# TYPE tdarr_server_info gauge
tdarr_server_info{os="linux",tdarr_instance="test-instance",version="2.77.01"} 1
# HELP tdarr_server_status_info Tdarr server self-reported health (value always 1); raw status string exposed as the 'status' label. Alert with tdarr_server_status_info{status!="good"} == 1.
# TYPE tdarr_server_status_info gauge
tdarr_server_status_info{status="good",tdarr_instance="test-instance"} 1
# HELP tdarr_server_uptime_seconds Tdarr server process uptime in seconds, as reported by /api/v2/status
# TYPE tdarr_server_uptime_seconds gauge
tdarr_server_uptime_seconds{tdarr_instance="test-instance"} 45
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected),
		"tdarr_server_uptime_seconds", "tdarr_server_info", "tdarr_server_status_info", "tdarr_server_healthy"); err != nil {
		t.Errorf("server metrics mismatch:\n%v", err)
	}
}

// TestCollect_ServerHealthy verifies tdarr_server_healthy maps Tdarr's self-reported
// status to 1 for known healthy synonyms (case-insensitive) and 0 for anything else.
func TestCollect_ServerHealthy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status string
		want   float64
	}{
		{"good", "good", 1},
		{"ok synonym", "ok", 1},
		{"healthy synonym", "healthy", 1},
		{"uppercase good", "GOOD", 1},
		{"surrounding whitespace", "  good  ", 1},
		{"unknown status", "degraded", 0},
		{"empty status", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := newTestConfig(t)
			api := newSuccessFakeAPI(cfg)
			body := fmt.Appendf(nil,
				`{"status":%q,"isProduction":true,"os":"linux","version":"2.77.01","buildDate":"x","uptime":45}`,
				tc.status)
			api.setResponse(fakeKey{path: cfg.TdarrStatusPath}, body)
			c := newTdarrCollectorWithAPI(cfg, api)

			expected := fmt.Sprintf(`
# HELP tdarr_server_healthy 1 if Tdarr server self-reported status is healthy ("good"/"ok"/"healthy", case-insensitive), 0 otherwise. Raw status string is on tdarr_server_status_info.
# TYPE tdarr_server_healthy gauge
tdarr_server_healthy{tdarr_instance="test-instance"} %g
`, tc.want)
			if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "tdarr_server_healthy"); err != nil {
				t.Errorf("server_healthy mismatch for status %q:\n%v", tc.status, err)
			}
		})
	}
}

// TestCollect_AllSuccess_UpEquals1 verifies tdarr_up == 1 when all API endpoints succeed,
// and that general stats metrics such as tdarr_files are emitted.
func TestCollect_AllSuccess_UpEquals1(t *testing.T) {
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, newSuccessFakeAPI(cfg))

	mfs := gatherMetricFamilies(t, c)
	if upValueFromFamilies(mfs) != 1.0 {
		t.Errorf("tdarr_up: want 1.0, got %v", upValueFromFamilies(mfs))
	}
	// Verify general stats are emitted — a regression that drops all data but keeps up=1 would fail here.
	if !hasMetricFamily(mfs, "tdarr_files") {
		t.Error("expected tdarr_files to be emitted on full success")
	}
}

// TestCollect_StatsAPIFails_UpEquals0 verifies tdarr_up == 0 when the stats-by-id cruddb
// call fails, and that no additional tdarr metrics beyond tdarr_up are emitted (early return path).
func TestCollect_StatsAPIFails_UpEquals0(t *testing.T) {
	cfg := newTestConfig(t)
	api := newSuccessFakeAPI(cfg)
	api.setError(fakeKey{path: cfg.TdarrStatsPath, disc: "StatisticsJSONDB"}, statErr{"stats fetch failed"})
	c := newTdarrCollectorWithAPI(cfg, api)

	mfs := gatherMetricFamilies(t, c)
	if upValueFromFamilies(mfs) != 0.0 {
		t.Errorf("tdarr_up: want 0.0, got %v", upValueFromFamilies(mfs))
	}
	if hasMetricFamily(mfs, "tdarr_files") {
		t.Error("unexpected tdarr_files emitted after early-return on stats failure")
	}
}

// TestCollect_LibraryListFails_UpEquals0 verifies tdarr_up == 0 when the library-list cruddb
// call (collection=LibrarySettingsJSONDB) fails while the stats-by-id call succeeds.
func TestCollect_LibraryListFails_UpEquals0(t *testing.T) {
	cfg := newTestConfig(t)
	api := newSuccessFakeAPI(cfg)
	api.setError(fakeKey{path: cfg.TdarrStatsPath, disc: "LibrarySettingsJSONDB"}, statErr{"library list failed"})
	c := newTdarrCollectorWithAPI(cfg, api)

	got := getUpValue(t, c)
	if got != 0.0 {
		t.Errorf("tdarr_up: want 0.0, got %v", got)
	}
}

// TestCollect_NodeFetchFails_UpEquals0 verifies tdarr_up == 0 when the node endpoint fails,
// and that general + pie stats metrics were still emitted before the node failure.
func TestCollect_NodeFetchFails_UpEquals0(t *testing.T) {
	cfg := newTestConfig(t)
	api := newSuccessFakeAPI(cfg)
	api.setError(fakeKey{path: cfg.TdarrNodePath}, statErr{"node fetch failed"})
	c := newTdarrCollectorWithAPI(cfg, api)

	mfs := gatherMetricFamilies(t, c)
	if upValueFromFamilies(mfs) != 0.0 {
		t.Errorf("tdarr_up: want 0.0, got %v", upValueFromFamilies(mfs))
	}
	if !hasMetricFamily(mfs, "tdarr_files") {
		t.Error("expected tdarr_files to be emitted despite node failure")
	}
}

// TestCollect_PartialPieFailure_UpEquals0 verifies tdarr_up == 0 when the get-pies call fails
// (the worker records the failure on the per-scrape partial flag returned by collect()),
// while general stats emitted before the pie fetch are still present.
func TestCollect_PartialPieFailure_UpEquals0(t *testing.T) {
	cfg := newTestConfig(t)
	api := newSuccessFakeAPI(cfg)
	api.setError(fakeKey{path: cfg.TdarrPieStatsPath, disc: "lib1"}, statErr{"pie fetch failed"})
	c := newTdarrCollectorWithAPI(cfg, api)

	mfs := gatherMetricFamilies(t, c)
	if upValueFromFamilies(mfs) != 0.0 {
		t.Errorf("tdarr_up: want 0.0, got %v", upValueFromFamilies(mfs))
	}
	// Partial failure means general stats succeeded before the pie fetch failed.
	// tdarr_files must still be emitted — that is the whole point of distinguishing
	// partial failure from a full stats fetch failure.
	if !hasMetricFamily(mfs, "tdarr_files") {
		t.Error("expected tdarr_files to be emitted despite partial pie failure")
	}
}

// TestCollect_ConsecutiveScrapes_PartialFlagResets is a critical regression test verifying
// that the partial-failure signal is scoped to a single scrape. Since collect() returns a
// local partial-failure bool per call (rather than storing it on a shared collector field),
// a failed scrape cannot leak its partial flag into the next scrape's result.
//
// If partial failure were instead tracked via a shared field across concurrent/consecutive
// scrapes, a stale true from a failed scrape could contaminate a later successful one:
// scrape 2 would still report tdarr_up == 0 even when all endpoints succeed.
func TestCollect_ConsecutiveScrapes_PartialFlagResets(t *testing.T) {
	cfg := newTestConfig(t)
	api := newSuccessFakeAPI(cfg)
	pieKey := fakeKey{path: cfg.TdarrPieStatsPath, disc: "lib1"}

	// Scrape 1: pie fails → tdarr_up should be 0.
	api.setError(pieKey, statErr{"pie fetch failed"})
	c := newTdarrCollectorWithAPI(cfg, api)

	// Use a fresh registry each scrape to avoid "already registered" errors.
	reg1 := prometheus.NewRegistry()
	reg1.MustRegister(c)
	mfs1, err := reg1.Gather()
	if err != nil {
		t.Fatalf("scrape 1 gather: %v", err)
	}
	if up1 := upValueFromFamilies(mfs1); up1 != 0.0 {
		t.Errorf("scrape 1 tdarr_up: want 0.0, got %v", up1)
	}

	// Flip to success mode for scrape 2. No manual cache reset needed: the
	// partial failure in scrape 1 already left the cache unwritten (see the
	// cache-write guard in collect()), so the collector re-fetches on its own.
	delete(api.errors, pieKey)

	// Scrape 2: all endpoints succeed → tdarr_up must be 1, not 0.
	reg2 := prometheus.NewRegistry()
	reg2.MustRegister(c)
	mfs2, err := reg2.Gather()
	if err != nil {
		t.Fatalf("scrape 2 gather: %v", err)
	}
	if up2 := upValueFromFamilies(mfs2); up2 != 1.0 {
		t.Errorf("scrape 2 tdarr_up: want 1.0, got %v (partialFailure flag leaked from scrape 1)", up2)
	}
}

// TestCollect_StatsScoreUnparseable_UpEquals0 verifies tdarr_up == 0 when the stats endpoint
// returns an unparseable tdarrScore string, and that no data metrics beyond tdarr_up are emitted
// (score parse failure in collect() returns early, before any ch <- emissions).
func TestCollect_StatsScoreUnparseable_UpEquals0(t *testing.T) {
	cfg := newTestConfig(t)
	api := newSuccessFakeAPI(cfg)
	m := TdarrMetric{TotalFileCount: 10, TdarrScore: "not-a-float", HealthCheckScore: "0"}
	b, _ := json.Marshal(m)
	api.setResponse(fakeKey{path: cfg.TdarrStatsPath, disc: "StatisticsJSONDB"}, b)
	c := newTdarrCollectorWithAPI(cfg, api)

	mfs := gatherMetricFamilies(t, c)
	if upValueFromFamilies(mfs) != 0.0 {
		t.Errorf("tdarr_up: want 0.0, got %v", upValueFromFamilies(mfs))
	}
	if hasMetricFamily(mfs, "tdarr_files") {
		t.Error("expected no tdarr_files: score parse failure should return before any data emissions")
	}
}

// TestCollect_HealthScoreUnparseable_UpEquals0 verifies tdarr_up == 0 when the stats endpoint
// returns an unparseable healthCheckScore string, and that no data metrics beyond tdarr_up are
// emitted (health score parse failure in collect() returns early, before any ch <- emissions).
func TestCollect_HealthScoreUnparseable_UpEquals0(t *testing.T) {
	cfg := newTestConfig(t)
	api := newSuccessFakeAPI(cfg)
	m := TdarrMetric{TotalFileCount: 10, TdarrScore: "0", HealthCheckScore: "garbage"}
	b, _ := json.Marshal(m)
	api.setResponse(fakeKey{path: cfg.TdarrStatsPath, disc: "StatisticsJSONDB"}, b)
	c := newTdarrCollectorWithAPI(cfg, api)

	mfs := gatherMetricFamilies(t, c)
	if upValueFromFamilies(mfs) != 0.0 {
		t.Errorf("tdarr_up: want 0.0, got %v", upValueFromFamilies(mfs))
	}
	if hasMetricFamily(mfs, "tdarr_files") {
		t.Error("expected no tdarr_files: health score parse failure should return before any data emissions")
	}
}

// TestCollect_ErrorCause verifies that collect() wraps the sentinel matching the
// failure category, so callers/tests can branch on the cause via errors.Is rather
// than matching error strings. Each case drives a distinct failure boundary.
func TestCollect_ErrorCause(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		setup     func(cfg config.Config, api *fakeTdarrAPI)
		wantCause error
	}{
		{
			name: "stats API failure is ErrUpstream",
			setup: func(cfg config.Config, api *fakeTdarrAPI) {
				api.setError(fakeKey{path: cfg.TdarrStatsPath, disc: "StatisticsJSONDB"}, statErr{"boom"})
			},
			wantCause: ErrUpstream,
		},
		{
			name: "node fetch failure is ErrUpstream",
			setup: func(cfg config.Config, api *fakeTdarrAPI) {
				api.setError(fakeKey{path: cfg.TdarrNodePath}, statErr{"boom"})
			},
			wantCause: ErrUpstream,
		},
		{
			name: "unparseable score is ErrParse",
			setup: func(cfg config.Config, api *fakeTdarrAPI) {
				m := TdarrMetric{TotalFileCount: 1, TdarrScore: "not-a-float", HealthCheckScore: "0"}
				b, _ := json.Marshal(m)
				api.setResponse(fakeKey{path: cfg.TdarrStatsPath, disc: "StatisticsJSONDB"}, b)
			},
			wantCause: ErrParse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := newTestConfig(t)
			api := newSuccessFakeAPI(cfg)
			tc.setup(cfg, api)
			c := newTdarrCollectorWithAPI(cfg, api)

			// Drain emissions into a buffered channel so collect() never blocks.
			ch := make(chan prometheus.Metric, 512)
			_, err := c.collect(context.Background(), ch)
			close(ch)

			if err == nil {
				t.Fatalf("collect: want error wrapping %v, got nil", tc.wantCause)
			}
			if !errors.Is(err, tc.wantCause) {
				t.Errorf("collect error = %v; want errors.Is(..., %v)", err, tc.wantCause)
			}
		})
	}
}

// TestCollect_InjectedLogger_CapturesOutput verifies the logger seam: a logger
// injected into the collector receives its log output, so tests can capture or
// silence logs without touching the package global. Drives the Collect error path
// and asserts the "Collection cycle failed" line lands in the injected buffer.
func TestCollect_InjectedLogger_CapturesOutput(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	api := newSuccessFakeAPI(cfg)
	api.setError(fakeKey{path: cfg.TdarrStatsPath, disc: "StatisticsJSONDB"}, statErr{"boom"})
	c := newTdarrCollectorWithAPI(cfg, api)

	var buf bytes.Buffer
	c.logger = zerolog.New(&buf)

	// Collect swallows the error after logging it via the injected logger.
	gatherMetricFamilies(t, c)

	if !strings.Contains(buf.String(), "Collection cycle failed") {
		t.Errorf("injected logger did not capture the failure log; buffer = %q", buf.String())
	}
}

// TestCollect_ContextCancelled_Aborts verifies the context seam: when the
// collector's baseCtx is already cancelled, the very first request aborts and
// collect() returns an error that carries both the cancellation cause and the
// ErrUpstream category — i.e. a scrape in flight at shutdown unwinds instead of
// running to completion.
func TestCollect_ContextCancelled_Aborts(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, newSuccessFakeAPI(cfg))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before collecting
	c.baseCtx = ctx

	ch := make(chan prometheus.Metric, 512)
	_, err := c.collect(ctx, ch)
	close(ch)

	if err == nil {
		t.Fatal("collect: want error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("collect error = %v; want errors.Is(..., context.Canceled)", err)
	}
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("collect error = %v; want errors.Is(..., ErrUpstream)", err)
	}
}

// TestCollect_RegisteredInRegistry_NoDescribeError verifies that registering TdarrCollector
// in a strict prometheus.Registry does not produce a describe/collect mismatch error.
// This guards against upMetric or any other descriptor being missing from Describe().
func TestCollect_RegisteredInRegistry_NoDescribeError(t *testing.T) {
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, newSuccessFakeAPI(cfg))

	reg := prometheus.NewRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := reg.Gather(); err != nil {
		t.Fatalf("gather: %v", err)
	}
}

// descFqNameRe extracts the fqName from a *prometheus.Desc's String() form, e.g.
// `Desc{fqName: "tdarr_files", ...}`. The fqName is not exported any other way.
var descFqNameRe = regexp.MustCompile(`fqName: "([^"]*)"`)

func descFqName(t *testing.T, d *prometheus.Desc) string {
	t.Helper()
	m := descFqNameRe.FindStringSubmatch(d.String())
	if m == nil {
		t.Fatalf("could not extract fqName from desc: %s", d.String())
	}
	return m[1]
}

// TestCollect_PanicInScrape_UpZeroAndNoGatherError verifies P1.4: a panic in the
// scrape path is recovered, tdarr_up is PRESENT with value 0.0 (not absent), and
// Gather returns no error — so the HTTP handler still serves 200 with tdarr_up=0
// rather than crashing the process or surfacing a 500.
func TestCollect_PanicInScrape_UpZeroAndNoGatherError(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, panicAPI{})

	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather returned error on recovered panic, want nil: %v", err)
	}
	if got := upValueFromFamilies(mfs); got != 0.0 {
		t.Errorf("tdarr_up: want 0.0 present, got %v (-1 means absent)", got)
	}
}

// TestCollect_PanicAfterPartialEmit_UpZeroWithPartialMetrics verifies the recover()
// path when the panic fires AFTER some metrics are already on ch: the status fetch
// succeeds and emitServerMetrics writes server series, then the stats POST panics.
// Gather must still return nil, tdarr_up must be PRESENT at 0.0, and the pre-panic
// server metric must survive (per-item Gather keeps already-accepted families).
func TestCollect_PanicAfterPartialEmit_UpZeroWithPartialMetrics(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, partialPanicAPI{statusBody: validStatusBody()})

	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather returned error on recovered mid-collect panic, want nil: %v", err)
	}
	if got := upValueFromFamilies(mfs); got != 0.0 {
		t.Errorf("tdarr_up: want 0.0 present, got %v (-1 means absent)", got)
	}
	if !hasMetricFamily(mfs, "tdarr_server_uptime_seconds") {
		t.Error("pre-panic server metric tdarr_server_uptime_seconds did not survive the recovered panic")
	}
}

// TestDescribe_EmitsAllDescs locks the Describe drift hazard: Prometheus does NOT flag a
// desc that silently drops out of Describe (it only errors on a Collect desc that was never
// described), so a missing Describe entry is invisible without an explicit count assertion.
// This test calls Describe, collects every emitted desc, and asserts both the exact total
// count (collector + node descs) and that a representative sample of fqNames is present.
func TestDescribe_EmitsAllDescs(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, newSuccessFakeAPI(cfg))

	// Drain Describe into a slice. Buffer generously so the send never blocks.
	ch := make(chan *prometheus.Desc, 256)
	c.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	fqNames := make(map[string]int)
	for d := range ch {
		descs = append(descs, d)
		fqNames[descFqName(t, d)]++
	}

	// 28 collector descs + 26 node descs. Adding/removing a metric must update this number,
	// which is exactly the point: the count is the tripwire for a dropped Describe entry.
	const wantCollectorDescs = 28
	const wantNodeDescs = 26
	wantTotal := wantCollectorDescs + wantNodeDescs
	if len(descs) != wantTotal {
		t.Fatalf("Describe emitted %d descs, want %d (collector %d + node %d)",
			len(descs), wantTotal, wantCollectorDescs, wantNodeDescs)
	}

	// No duplicate fqName should appear — each metric is described exactly once.
	for name, n := range fqNames {
		if n != 1 {
			t.Errorf("fqName %q described %d times, want 1", name, n)
		}
	}

	// Representative sample spanning collector totals, the up gauge, the drift counter,
	// and both node-level and worker-level node descs.
	wantPresent := []string{
		"tdarr_files",
		"tdarr_up",
		"tdarr_unknown_status_total",
		"tdarr_library_audio_containers",
		"tdarr_node_info",
		"tdarr_node_worker_info",
	}
	for _, name := range wantPresent {
		if fqNames[name] == 0 {
			t.Errorf("Describe missing expected desc %q", name)
		}
	}
}

// TestCollect_PartialPieFailure_DoesNotPoisonCache verifies that a partial
// per-library pie fetch failure does not write incomplete data into the stats
// cache. Regression test: previously the partial result was cached together
// with the current totals, so the NEXT scrape hit the cache, served incomplete
// data (lib2's series missing), and reported tdarr_up=1 — masking the gap
// until the totals next changed.
//
// Two libraries are required: lib1 succeeds, lib2 fails. With a single library
// the partial result is empty (nil), nothing meaningful is cached, and the bug
// cannot be observed.
func TestCollect_PartialPieFailure_DoesNotPoisonCache(t *testing.T) {
	cfg := newTestConfig(t)
	api := newSuccessFakeAPI(cfg)
	twoLibs, _ := json.Marshal([]TdarrLibraryInfo{
		{LibraryId: "lib1", Name: "Library One"},
		{LibraryId: "lib2", Name: "Library Two"},
	})
	api.setResponse(fakeKey{path: cfg.TdarrStatsPath, disc: "LibrarySettingsJSONDB"}, twoLibs)
	// Register lib2's success response up front; the error registered next takes
	// precedence (see fakeTdarrAPI.setError), and deleting the error later
	// reveals the response for scrape 2.
	pieKey2 := fakeKey{path: cfg.TdarrPieStatsPath, disc: "lib2"}
	api.setResponse(pieKey2, validPieBody())
	api.setError(pieKey2, statErr{"pie fetch failed"})
	c := newTdarrCollectorWithAPI(cfg, api)

	// Scrape 1: lib1 ok, lib2 fails → up=0 and, critically, cache stays empty.
	reg1 := prometheus.NewRegistry()
	reg1.MustRegister(c)
	mfs1, err := reg1.Gather()
	if err != nil {
		t.Fatalf("scrape 1 gather: %v", err)
	}
	if up := upValueFromFamilies(mfs1); up != 0.0 {
		t.Errorf("scrape 1 tdarr_up: want 0.0, got %v", up)
	}
	if _, stats := c.statsCache.Read(); stats != nil {
		t.Fatal("cache was written despite partial pie failure; partial data must not be cached")
	}

	// Scrape 2: lib2 recovers. NO manual cache reset — the collector must
	// refetch on its own because the cache was never written.
	delete(api.errors, pieKey2)
	reg2 := prometheus.NewRegistry()
	reg2.MustRegister(c)
	mfs2, err := reg2.Gather()
	if err != nil {
		t.Fatalf("scrape 2 gather: %v", err)
	}
	if up := upValueFromFamilies(mfs2); up != 1.0 {
		t.Errorf("scrape 2 tdarr_up: want 1.0, got %v", up)
	}
	if got := libraryFilesSeriesCount(mfs2); got != 2 {
		t.Errorf("scrape 2 tdarr_library_files series: want 2 (lib1+lib2), got %d", got)
	}
	if _, stats := c.statsCache.Read(); stats == nil {
		t.Error("cache not written after fully successful scrape")
	}
}

// libraryFilesSeriesCount returns the number of series in the
// tdarr_library_files family (one per library emitted).
func libraryFilesSeriesCount(mfs []*dto.MetricFamily) int {
	for _, mf := range mfs {
		if mf.GetName() == "tdarr_library_files" {
			return len(mf.GetMetric())
		}
	}
	return 0
}

// TestLibStatsCache_ReadWritePairing verifies Read always returns the exact
// totals/stats pair the last Write stored, sequentially. This is the baseline
// invariant check for the single-lock Read/Write API replacing the four
// separately-locked getters/setters.
func TestLibStatsCache_ReadWritePairing(t *testing.T) {
	c := NewTdarrLibStatsCache()
	// empty cache: stats nil (refetch signal), zero totals
	if tot, stats := c.Read(); stats != nil || tot != (tdarrCacheTotals{}) {
		t.Fatalf("empty cache: want zero totals + nil stats, got %+v / %v", tot, stats)
	}
	totals := tdarrCacheTotals{totalFileCount: 42}
	stats := []*TdarrPieStats{{libraryId: "lib1"}}
	c.Write(totals, stats)
	gotT, gotS := c.Read()
	if gotT != totals {
		t.Errorf("totals: want %+v, got %+v", totals, gotT)
	}
	if len(gotS) != 1 || gotS[0].libraryId != "lib1" {
		t.Errorf("stats: want [lib1], got %v", gotS)
	}
}

// TestLibStatsCache_ReadWritePairing_Concurrent races N writers against N
// readers under -race. Each writer stores a totals/stats pair whose values
// are derived from the same index, so any Read that observed a torn pair
// (totals from one Write, stats from another) would show a mismatched index.
func TestLibStatsCache_ReadWritePairing_Concurrent(t *testing.T) {
	c := NewTdarrLibStatsCache()
	const n = 100

	var wg sync.WaitGroup
	wg.Add(2 * n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			c.Write(
				tdarrCacheTotals{totalFileCount: i},
				[]*TdarrPieStats{{libraryId: fmt.Sprintf("lib%d", i)}},
			)
		}()
		go func() {
			defer wg.Done()
			tot, stats := c.Read()
			if stats == nil {
				return // cache not yet written; not a torn pair
			}
			id := strings.TrimPrefix(stats[0].libraryId, "lib")
			want, err := strconv.Atoi(id)
			if err != nil {
				t.Errorf("unexpected libraryId format %q: %v", stats[0].libraryId, err)
				return
			}
			if tot.totalFileCount != want {
				t.Errorf("torn pair: totals.totalFileCount=%d does not match stats libraryId=%q", tot.totalFileCount, stats[0].libraryId)
			}
		}()
	}
	wg.Wait()
}
