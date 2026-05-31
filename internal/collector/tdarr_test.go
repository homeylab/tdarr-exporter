package collector

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"testing"

	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
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

// newSuccessFakeAPI builds a fakeTdarrAPI that responds successfully to every
// endpoint the collector calls, using the minimal valid bodies above. The single
// library "lib1" drives one get-pies call keyed on its libraryId.
func newSuccessFakeAPI(cfg config.Config) *fakeTdarrAPI {
	api := newFakeTdarrAPI()
	api.setResponse(fakeKey{path: cfg.TdarrStatsPath, disc: "StatisticsJSONDB"}, validStatsBody())
	api.setResponse(fakeKey{path: cfg.TdarrStatsPath, disc: "LibrarySettingsJSONDB"}, validLibraryListBody())
	api.setResponse(fakeKey{path: cfg.TdarrPieStatsPath, disc: "lib1"}, validPieBody())
	api.setResponse(fakeKey{path: cfg.TdarrNodePath}, validNodeBody())
	return api
}

// TestCollect_AllSuccess_UpEquals1 verifies tdarr_up == 1 when all API endpoints succeed,
// and that general stats metrics such as tdarr_files_total are emitted.
func TestCollect_AllSuccess_UpEquals1(t *testing.T) {
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, newSuccessFakeAPI(cfg))

	mfs := gatherMetricFamilies(t, c)
	if upValueFromFamilies(mfs) != 1.0 {
		t.Errorf("tdarr_up: want 1.0, got %v", upValueFromFamilies(mfs))
	}
	// Verify general stats are emitted — a regression that drops all data but keeps up=1 would fail here.
	if !hasMetricFamily(mfs, "tdarr_files_total") {
		t.Error("expected tdarr_files_total to be emitted on full success")
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
	if hasMetricFamily(mfs, "tdarr_files_total") {
		t.Error("unexpected tdarr_files_total emitted after early-return on stats failure")
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
	if !hasMetricFamily(mfs, "tdarr_files_total") {
		t.Error("expected tdarr_files_total to be emitted despite node failure")
	}
}

// TestCollect_PartialPieFailure_UpEquals0 verifies tdarr_up == 0 when the get-pies call fails
// (the worker records a partial failure via c.partialFailure.Store(true)), while general stats
// emitted before the pie fetch are still present.
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
	// tdarr_files_total must still be emitted — that is the whole point of distinguishing
	// partial failure from a full stats fetch failure.
	if !hasMetricFamily(mfs, "tdarr_files_total") {
		t.Error("expected tdarr_files_total to be emitted despite partial pie failure")
	}
}

// TestCollect_ConsecutiveScrapes_PartialFlagResets is a critical regression test for the
// partialFailure.Swap(false) fix in Collect(). It verifies that a stale partialFailure flag
// from a failed scrape does not contaminate the following successful scrape.
//
// If the flag were reset via short-circuit OR (e.g. `err != nil || partial` before Swap), the
// flag would leak across scrapes: scrape 2 would still report tdarr_up == 0 even when all
// endpoints succeed.
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

	// Flip to success mode for scrape 2.
	delete(api.errors, pieKey)
	// Reset the stats cache so the collector re-fetches library pie stats
	// (cache hit avoids pie calls entirely, which would skip the test's purpose).
	c.statsCache = NewTdarrLibStatsCache()

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
	if hasMetricFamily(mfs, "tdarr_files_total") {
		t.Error("expected no tdarr_files_total: score parse failure should return before any data emissions")
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
	if hasMetricFamily(mfs, "tdarr_files_total") {
		t.Error("expected no tdarr_files_total: health score parse failure should return before any data emissions")
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := newTestConfig(t)
			api := newSuccessFakeAPI(cfg)
			tc.setup(cfg, api)
			c := newTdarrCollectorWithAPI(cfg, api)

			// Drain emissions into a buffered channel so collect() never blocks.
			ch := make(chan prometheus.Metric, 512)
			err := c.collect(context.Background(), ch)
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
	err := c.collect(ctx, ch)
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
// `Desc{fqName: "tdarr_files_total", ...}`. The fqName is not exported any other way.
var descFqNameRe = regexp.MustCompile(`fqName: "([^"]*)"`)

func descFqName(t *testing.T, d *prometheus.Desc) string {
	t.Helper()
	m := descFqNameRe.FindStringSubmatch(d.String())
	if m == nil {
		t.Fatalf("could not extract fqName from desc: %s", d.String())
	}
	return m[1]
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

	// 24 collector descs + 24 node descs. Adding/removing a metric must update this number,
	// which is exactly the point: the count is the tripwire for a dropped Describe entry.
	const wantCollectorDescs = 24
	const wantNodeDescs = 24
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
		"tdarr_files_total",
		"tdarr_up",
		"tdarr_unknown_status_total",
		"tdarr_library_audio_resolutions",
		"tdarr_node_info",
		"tdarr_node_worker_info",
	}
	for _, name := range wantPresent {
		if fqNames[name] == 0 {
			t.Errorf("Describe missing expected desc %q", name)
		}
	}
}
