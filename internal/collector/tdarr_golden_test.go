package collector

import (
	"net/url"
	"os"
	"testing"

	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// collectorMetricNames is the exhaustive list of metric families emitted by
// TdarrCollector.Collect.  Metrics that come from internal/handlers/metrics.go
// (tdarr_scrape_duration_seconds) are intentionally excluded so they cannot
// influence the comparison.
var collectorMetricNames = []string{
	"tdarr_avg_num_streams",
	"tdarr_files",
	"tdarr_health_check_score_ratio",
	"tdarr_health_checks_completed_total",
	"tdarr_library_audio_codecs",
	"tdarr_library_audio_containers",
	"tdarr_library_files",
	"tdarr_library_health_checks",
	"tdarr_library_health_checks_completed_total",
	"tdarr_library_info",
	"tdarr_library_size_diff_bytes",
	"tdarr_library_transcodes",
	"tdarr_library_transcodes_completed_total",
	"tdarr_library_video_codecs",
	"tdarr_library_video_containers",
	"tdarr_library_video_resolutions",
	"tdarr_node_heap_total_bytes",
	"tdarr_node_heap_used_bytes",
	"tdarr_node_host_cpu_ratio",
	"tdarr_node_host_mem_total_bytes",
	"tdarr_node_host_mem_used_bytes",
	"tdarr_node_info",
	"tdarr_node_max_gpu_workers",
	"tdarr_node_paused",
	"tdarr_node_queue_length",
	"tdarr_node_schedule_enabled",
	"tdarr_node_uptime_seconds",
	"tdarr_node_worker_count",
	"tdarr_node_worker_est_file_size_bytes",
	"tdarr_node_worker_eta_seconds",
	"tdarr_node_worker_fps",
	"tdarr_node_worker_idle",
	"tdarr_node_worker_info",
	"tdarr_node_worker_job_start_timestamp_seconds",
	"tdarr_node_worker_limit",
	"tdarr_node_worker_original_file_size_bytes",
	"tdarr_node_worker_output_file_size_bytes",
	"tdarr_node_worker_ratio",
	"tdarr_node_worker_plugin",
	"tdarr_node_worker_status",
	"tdarr_node_worker_status_timestamp_seconds",
	"tdarr_node_worker_step_start_timestamp_seconds",
	"tdarr_score_ratio",
	"tdarr_server_healthy",
	"tdarr_server_info",
	"tdarr_server_status_info",
	"tdarr_server_uptime_seconds",
	"tdarr_size_diff_bytes",
	"tdarr_stream_stats_bit_rate",
	"tdarr_stream_stats_duration_seconds",
	"tdarr_stream_stats_num_frames",
	"tdarr_transcodes_completed_total",
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

// newGoldenTestConfig returns a config.Config with deterministic settings.
// HttpMaxConcurrency=1 ensures per-library pie requests are processed sequentially,
// making pieData slice ordering fully deterministic. UrlParsed is set so the config
// is well-formed even though the injected fake never makes a network call.
func newGoldenTestConfig(t *testing.T) config.Config {
	t.Helper()
	u, err := url.Parse("http://tdarr.test")
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
		TdarrStatusPath:    "/api/v2/status",
		HttpMaxConcurrency: 1,
	}
}

// newGoldenFakeAPI builds a fakeTdarrAPI backed by the testdata fixtures, mirroring
// the routing the real Tdarr API performs:
//   - POST /api/v2/cruddb  collection=StatisticsJSONDB      → general_stats.json
//   - POST /api/v2/cruddb  collection=LibrarySettingsJSONDB → library_list.json
//   - POST /api/v2/stats/get-pies  libraryId=lib-video-01   → pie_stats_lib_video_01.json
//   - POST /api/v2/stats/get-pies  libraryId=lib-audio-01   → pie_stats_lib_audio_01.json
//   - GET  /api/v2/get-nodes                                → nodes.json
func newGoldenFakeAPI(t *testing.T, cfg config.Config) *fakeTdarrAPI {
	t.Helper()
	api := newFakeTdarrAPI()
	api.setResponse(fakeKey{path: cfg.TdarrStatsPath, disc: "StatisticsJSONDB"}, readFixture(t, "general_stats.json"))
	api.setResponse(fakeKey{path: cfg.TdarrStatsPath, disc: "LibrarySettingsJSONDB"}, readFixture(t, "library_list.json"))
	api.setResponse(fakeKey{path: cfg.TdarrPieStatsPath, disc: "lib-video-01"}, readFixture(t, "pie_stats_lib_video_01.json"))
	api.setResponse(fakeKey{path: cfg.TdarrPieStatsPath, disc: "lib-audio-01"}, readFixture(t, "pie_stats_lib_audio_01.json"))
	api.setResponse(fakeKey{path: cfg.TdarrNodePath}, readFixture(t, "nodes.json"))
	api.setResponse(fakeKey{path: cfg.TdarrStatusPath}, readFixture(t, "server_status.json"))
	return api
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
//     percentage, fps, original/output/est file sizes, job_start/start/status timestamps, eta_seconds
//   - tdarr_up = 1 on success path
func TestCollect_Golden_FullFixture(t *testing.T) {
	cfg := newGoldenTestConfig(t)
	api := newGoldenFakeAPI(t, cfg)
	collector := newTdarrCollectorWithAPI(cfg, api)

	expectedFile, err := os.Open("testdata/expected_output.txt")
	if err != nil {
		t.Fatalf("open expected output: %v", err)
	}
	defer func() { _ = expectedFile.Close() }()

	if err := testutil.CollectAndCompare(collector, expectedFile, collectorMetricNames...); err != nil {
		t.Errorf("metric output mismatch:\n%v", err)
	}
}
