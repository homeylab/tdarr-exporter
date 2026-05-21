package collector

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// newTestConfig builds a Config pointing at serverURL with sane test defaults.
func newTestConfig(t *testing.T, serverURL string) config.Config {
	t.Helper()
	u, err := url.Parse(serverURL)
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
		HttpMaxConcurrency: 2,
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

// parseCruddbMode reads the request body and extracts the "mode" field from the nested data object.
func parseCruddbMode(r *http.Request) string {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}
	var req TdarrMetricRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Data.Mode
}

// newFullSuccessServer builds a fake Tdarr API server that responds successfully to all endpoints.
func newFullSuccessServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v2/cruddb", func(w http.ResponseWriter, r *http.Request) {
		mode := parseCruddbMode(r)
		w.Header().Set("Content-Type", "application/json")
		if mode == "getAll" {
			_, _ = w.Write(validLibraryListBody())
		} else {
			_, _ = w.Write(validStatsBody())
		}
	})

	mux.HandleFunc("/api/v2/stats/get-pies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validPieBody())
	})

	mux.HandleFunc("/api/v2/get-nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validNodeBody())
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestCollect_AllSuccess_UpEquals1 verifies tdarr_up == 1 when all API endpoints succeed,
// and that general stats metrics such as tdarr_files_total are emitted.
func TestCollect_AllSuccess_UpEquals1(t *testing.T) {
	srv := newFullSuccessServer(t)
	cfg := newTestConfig(t, srv.URL)
	c := NewTdarrCollector(cfg)

	mfs := gatherMetricFamilies(t, c)
	if upValueFromFamilies(mfs) != 1.0 {
		t.Errorf("tdarr_up: want 1.0, got %v", upValueFromFamilies(mfs))
	}
	// Verify general stats are emitted — a regression that drops all data but keeps up=1 would fail here.
	if !hasMetricFamily(mfs, "tdarr_files_total") {
		t.Error("expected tdarr_files_total to be emitted on full success")
	}
}

// TestCollect_StatsAPIReturns404_UpEquals0 verifies tdarr_up == 0 when the stats endpoint fails,
// and that no additional tdarr metrics beyond tdarr_up are emitted (early return path).
func TestCollect_StatsAPIReturns404_UpEquals0(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/cruddb", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	mux.HandleFunc("/api/v2/stats/get-pies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validPieBody())
	})
	mux.HandleFunc("/api/v2/get-nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validNodeBody())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := newTestConfig(t, srv.URL)
	c := NewTdarrCollector(cfg)

	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	upVal := -1.0
	for _, mf := range mfs {
		if mf.GetName() == "tdarr_up" {
			upVal = mf.GetMetric()[0].GetGauge().GetValue()
		} else if mf.GetName() == "tdarr_files_total" {
			t.Errorf("unexpected metric emitted after early-return: %s", mf.GetName())
		}
	}
	if upVal != 0.0 {
		t.Errorf("tdarr_up: want 0.0, got %v", upVal)
	}
}

// TestCollect_LibraryListFails_UpEquals0 verifies tdarr_up == 0 when the library-list cruddb
// call (mode=getAll) fails while the stats-by-id call succeeds.
func TestCollect_LibraryListFails_UpEquals0(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/cruddb", func(w http.ResponseWriter, r *http.Request) {
		mode := parseCruddbMode(r)
		if mode == "getAll" {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validStatsBody())
	})
	mux.HandleFunc("/api/v2/stats/get-pies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validPieBody())
	})
	mux.HandleFunc("/api/v2/get-nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validNodeBody())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := newTestConfig(t, srv.URL)
	c := NewTdarrCollector(cfg)

	got := getUpValue(t, c)
	if got != 0.0 {
		t.Errorf("tdarr_up: want 0.0, got %v", got)
	}
}

// TestCollect_NodeFetchFails_UpEquals0 verifies tdarr_up == 0 when the node endpoint fails,
// and that general + pie stats metrics were still emitted before the node failure.
func TestCollect_NodeFetchFails_UpEquals0(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/cruddb", func(w http.ResponseWriter, r *http.Request) {
		mode := parseCruddbMode(r)
		w.Header().Set("Content-Type", "application/json")
		if mode == "getAll" {
			_, _ = w.Write(validLibraryListBody())
		} else {
			_, _ = w.Write(validStatsBody())
		}
	})
	mux.HandleFunc("/api/v2/stats/get-pies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validPieBody())
	})
	mux.HandleFunc("/api/v2/get-nodes", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := newTestConfig(t, srv.URL)
	c := NewTdarrCollector(cfg)

	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	upVal := -1.0
	hasTotalFiles := false
	for _, mf := range mfs {
		switch mf.GetName() {
		case "tdarr_up":
			upVal = mf.GetMetric()[0].GetGauge().GetValue()
		case "tdarr_files_total":
			hasTotalFiles = true
		}
	}
	if upVal != 0.0 {
		t.Errorf("tdarr_up: want 0.0, got %v", upVal)
	}
	if !hasTotalFiles {
		t.Error("expected tdarr_files_total to be emitted despite node failure")
	}
}

// TestCollect_PartialPieFailure_UpEquals0 verifies tdarr_up == 0 when all get-pies calls fail
// (all workers encounter errors, setting partialFailure = true).
// Note: returning 500 for all get-pies requests triggers the partial failure path since every
// worker goroutine records a failure via c.partialFailure.Store(true).
func TestCollect_PartialPieFailure_UpEquals0(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/cruddb", func(w http.ResponseWriter, r *http.Request) {
		mode := parseCruddbMode(r)
		w.Header().Set("Content-Type", "application/json")
		if mode == "getAll" {
			_, _ = w.Write(validLibraryListBody())
		} else {
			_, _ = w.Write(validStatsBody())
		}
	})
	mux.HandleFunc("/api/v2/stats/get-pies", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	mux.HandleFunc("/api/v2/get-nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validNodeBody())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := newTestConfig(t, srv.URL)
	c := NewTdarrCollector(cfg)

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
	// pieFailure controls whether get-pies returns an error. 1 = fail, 0 = succeed.
	var pieFailure atomic.Int32
	pieFailure.Store(1) // scrape 1: fail

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/cruddb", func(w http.ResponseWriter, r *http.Request) {
		mode := parseCruddbMode(r)
		w.Header().Set("Content-Type", "application/json")
		if mode == "getAll" {
			_, _ = w.Write(validLibraryListBody())
		} else {
			_, _ = w.Write(validStatsBody())
		}
	})
	mux.HandleFunc("/api/v2/stats/get-pies", func(w http.ResponseWriter, r *http.Request) {
		if pieFailure.Load() == 1 {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validPieBody())
	})
	mux.HandleFunc("/api/v2/get-nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validNodeBody())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := newTestConfig(t, srv.URL)
	c := NewTdarrCollector(cfg)

	// Scrape 1: pie fails → tdarr_up should be 0.
	// Use a fresh registry each scrape to avoid "already registered" errors.
	reg1 := prometheus.NewRegistry()
	reg1.MustRegister(c)
	mfs1, err := reg1.Gather()
	if err != nil {
		t.Fatalf("scrape 1 gather: %v", err)
	}
	up1 := -1.0
	for _, mf := range mfs1 {
		if mf.GetName() == "tdarr_up" {
			up1 = mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	if up1 != 0.0 {
		t.Errorf("scrape 1 tdarr_up: want 0.0, got %v", up1)
	}

	// Flip to success mode for scrape 2.
	pieFailure.Store(0)
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
	up2 := -1.0
	for _, mf := range mfs2 {
		if mf.GetName() == "tdarr_up" {
			up2 = mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	if up2 != 1.0 {
		t.Errorf("scrape 2 tdarr_up: want 1.0, got %v (partialFailure flag leaked from scrape 1)", up2)
	}
}

// TestCollect_StatsScoreUnparseable_UpEquals0 verifies tdarr_up == 0 when the stats endpoint
// returns an unparseable tdarrScore string, and that no data metrics beyond tdarr_up are emitted
// (score parse failure in collect() returns early, before any ch <- emissions).
func TestCollect_StatsScoreUnparseable_UpEquals0(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/cruddb", func(w http.ResponseWriter, r *http.Request) {
		mode := parseCruddbMode(r)
		w.Header().Set("Content-Type", "application/json")
		if mode == "getAll" {
			_, _ = w.Write(validLibraryListBody())
		} else {
			m := TdarrMetric{
				TotalFileCount:   10,
				TdarrScore:       "not-a-float",
				HealthCheckScore: "0",
			}
			b, _ := json.Marshal(m)
			_, _ = w.Write(b)
		}
	})
	mux.HandleFunc("/api/v2/stats/get-pies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validPieBody())
	})
	mux.HandleFunc("/api/v2/get-nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validNodeBody())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := newTestConfig(t, srv.URL)
	c := NewTdarrCollector(cfg)

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
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/cruddb", func(w http.ResponseWriter, r *http.Request) {
		mode := parseCruddbMode(r)
		w.Header().Set("Content-Type", "application/json")
		if mode == "getAll" {
			_, _ = w.Write(validLibraryListBody())
		} else {
			m := TdarrMetric{
				TotalFileCount:   10,
				TdarrScore:       "0",
				HealthCheckScore: "garbage",
			}
			b, _ := json.Marshal(m)
			_, _ = w.Write(b)
		}
	})
	mux.HandleFunc("/api/v2/stats/get-pies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validPieBody())
	})
	mux.HandleFunc("/api/v2/get-nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validNodeBody())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := newTestConfig(t, srv.URL)
	c := NewTdarrCollector(cfg)

	mfs := gatherMetricFamilies(t, c)
	if upValueFromFamilies(mfs) != 0.0 {
		t.Errorf("tdarr_up: want 0.0, got %v", upValueFromFamilies(mfs))
	}
	if hasMetricFamily(mfs, "tdarr_files_total") {
		t.Error("expected no tdarr_files_total: health score parse failure should return before any data emissions")
	}
}

// TestCollect_RegisteredInRegistry_NoDescribeError verifies that registering TdarrCollector
// in a strict prometheus.Registry does not produce a describe/collect mismatch error.
// This guards against upMetric or any other descriptor being missing from Describe().
func TestCollect_RegisteredInRegistry_NoDescribeError(t *testing.T) {
	srv := newFullSuccessServer(t)
	cfg := newTestConfig(t, srv.URL)
	c := NewTdarrCollector(cfg)

	reg := prometheus.NewRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := reg.Gather(); err != nil {
		t.Fatalf("gather: %v", err)
	}
}
