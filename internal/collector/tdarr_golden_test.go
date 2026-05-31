package collector

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// collectorMetricNames is the exhaustive list of metric families emitted by
// TdarrCollector.Collect.  Metrics that come from internal/handlers/metrics.go
// (tdarr_scrape_duration_seconds, tdarr_scrape_requests_total) are intentionally
// excluded so they cannot influence the comparison.
var collectorMetricNames = []string{
	"tdarr_avg_num_streams",
	"tdarr_files_total",
	"tdarr_health_check_score_pct",
	"tdarr_health_checks_total",
	"tdarr_library_audio_codecs",
	"tdarr_library_audio_containers",
	"tdarr_library_files_total",
	"tdarr_library_health_checks",
	"tdarr_library_health_checks_total",
	"tdarr_library_size_diff_gb",
	"tdarr_library_transcodes",
	"tdarr_library_transcodes_total",
	"tdarr_library_video_codecs",
	"tdarr_library_video_containers",
	"tdarr_library_video_resolutions",
	"tdarr_node_heap_total_mb",
	"tdarr_node_heap_used_mb",
	"tdarr_node_host_cpu_percent",
	"tdarr_node_host_mem_total_gb",
	"tdarr_node_host_mem_used_gb",
	"tdarr_node_info",
	"tdarr_node_max_gpu_workers",
	"tdarr_node_paused",
	"tdarr_node_queue_length",
	"tdarr_node_schedule_enabled",
	"tdarr_node_uptime_seconds",
	"tdarr_node_worker_count",
	"tdarr_node_worker_est_file_size_gb",
	"tdarr_node_worker_eta_seconds",
	"tdarr_node_worker_fps",
	"tdarr_node_worker_info",
	"tdarr_node_worker_job_start_timestamp_seconds",
	"tdarr_node_worker_limit",
	"tdarr_node_worker_original_file_size_gb",
	"tdarr_node_worker_output_file_size_gb",
	"tdarr_node_worker_percentage",
	"tdarr_node_worker_pid",
	"tdarr_node_worker_start_timestamp_seconds",
	"tdarr_node_worker_status_timestamp_seconds",
	"tdarr_score_pct",
	"tdarr_size_diff_gb",
	"tdarr_stream_stats_bit_rate",
	"tdarr_stream_stats_duration",
	"tdarr_stream_stats_num_frames",
	"tdarr_transcodes_total",
	"tdarr_unknown_status_total",
	"tdarr_up",
}

// readFixture reads a file from testdata/ relative to the test file location.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// newGoldenTestServer builds a fake Tdarr API server backed by testdata fixtures.
// Routing:
//   - POST /api/v2/cruddb  collection=StatisticsJSONDB  → general_stats.json
//   - POST /api/v2/cruddb  collection=LibrarySettingsJSONDB → library_list.json
//   - POST /api/v2/stats/get-pies  libraryId=lib-video-01 → pie_stats_lib_video_01.json
//   - POST /api/v2/stats/get-pies  libraryId=lib-audio-01 → pie_stats_lib_audio_01.json
//   - GET  /api/v2/get-nodes → nodes.json
func newGoldenTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	generalStats := readFixture(t, "general_stats.json")
	libraryList := readFixture(t, "library_list.json")
	pieVideo := readFixture(t, "pie_stats_lib_video_01.json")
	pieAudio := readFixture(t, "pie_stats_lib_audio_01.json")
	nodesData := readFixture(t, "nodes.json")

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v2/cruddb", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body error", http.StatusBadRequest)
			return
		}
		var req TdarrMetricRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "unmarshal error", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Data.Collection {
		case "LibrarySettingsJSONDB":
			_, _ = w.Write(libraryList)
		default: // StatisticsJSONDB
			_, _ = w.Write(generalStats)
		}
	})

	mux.HandleFunc("/api/v2/stats/get-pies", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body error", http.StatusBadRequest)
			return
		}
		var req TdarrPieDataRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "unmarshal error", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Data.LibraryId {
		case "lib-audio-01":
			_, _ = w.Write(pieAudio)
		default: // lib-video-01
			_, _ = w.Write(pieVideo)
		}
	})

	mux.HandleFunc("/api/v2/get-nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(nodesData)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newGoldenTestConfig returns a config.Config pointing at serverURL with deterministic settings.
// HttpMaxConcurrency=1 ensures per-library pie requests are processed sequentially,
// making pieData slice ordering fully deterministic.
func newGoldenTestConfig(t *testing.T, serverURL string) config.Config {
	t.Helper()
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return config.Config{
		UrlParsed:          u,
		InstanceName:       "tdarr.localdomain",
		ApiKey:             "",
		VerifySsl:          false,
		HttpTimeoutSeconds: 5,
		TdarrStatsPath:     "/api/v2/cruddb",
		TdarrPieStatsPath:  "/api/v2/stats/get-pies",
		TdarrNodePath:      "/api/v2/get-nodes",
		HttpMaxConcurrency: 1,
	}
}

// TestCollect_Golden_FullFixture is a characterization test that pins the exact
// Prometheus output of TdarrCollector.Collect against testdata/expected_output.txt.
//
// Fixture paths exercised:
//   - general stats + stream stats (all fields)
//   - two libraries: "Shows" (video-only) and "Music" (with audio codecs + audio containers)
//   - all known transcode statuses (not required, success, error, queued, hold, ignored, cancelled)
//   - unknown transcode status "pending" → exercises tdarr_unknown_status_total
//   - all known health check statuses (success, error, queued, cancelled)
//   - video codecs, containers, resolutions (lowercased from mixed-case fixture values)
//   - audio codecs (aac, flac, opus) and audio containers (mkv, m4a) for "Music" library
//   - idle node "IdleNode" with no workers (zero-value worker count/limit/queue series emitted)
//   - busy node "BusyNode" with one active transcode CPU worker exercising all per-worker gauges:
//     percentage, fps, original/output/est file sizes, job_start/start/status timestamps, pid, eta_seconds
//   - tdarr_up = 1 on success path
func TestCollect_Golden_FullFixture(t *testing.T) {
	srv := newGoldenTestServer(t)
	cfg := newGoldenTestConfig(t, srv.URL)
	collector := NewTdarrCollector(cfg)

	expectedFile, err := os.Open("testdata/expected_output.txt")
	if err != nil {
		t.Fatalf("open expected output: %v", err)
	}
	defer expectedFile.Close()

	if err := testutil.CollectAndCompare(collector, expectedFile, collectorMetricNames...); err != nil {
		t.Errorf("metric output mismatch:\n%v", err)
	}
}
