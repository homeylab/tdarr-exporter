package collector

import (
	"sort"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// sample is a flattened view of one emitted prometheus.Metric: the descriptor's
// fqName, its label set (name->value, including the const tdarr_instance label), and
// the numeric value. It lets table tests assert the full emitted series set without
// depending on emission order.
type sample struct {
	fqName string
	labels map[string]string
	value  float64
}

// drain converts the metrics sent to ch (the channel must already be closed) into
// flattened samples. fqName is parsed out of the Desc string since *prometheus.Desc
// exposes no public fqName accessor.
func drain(t *testing.T, ch <-chan prometheus.Metric) []sample {
	t.Helper()
	var out []sample
	for m := range ch {
		var pb dto.Metric
		if err := m.Write(&pb); err != nil {
			t.Fatalf("write metric: %v", err)
		}
		labels := map[string]string{}
		for _, lp := range pb.GetLabel() {
			labels[lp.GetName()] = lp.GetValue()
		}
		var val float64
		switch {
		case pb.GetGauge() != nil:
			val = pb.GetGauge().GetValue()
		case pb.GetCounter() != nil:
			val = pb.GetCounter().GetValue()
		default:
			t.Fatalf("unexpected metric type for %s", m.Desc().String())
		}
		out = append(out, sample{
			fqName: fqNameFromDesc(m.Desc().String()),
			labels: labels,
			value:  val,
		})
	}
	return out
}

// fqNameFromDesc extracts the fqName from a Desc.String() rendering, which looks like:
//
//	Desc{fqName: "tdarr_files_total", help: "...", ...}
func fqNameFromDesc(desc string) string {
	const marker = `fqName: "`
	i := len(marker)
	start := indexOf(desc, marker)
	if start < 0 {
		return desc
	}
	rest := desc[start+i:]
	end := indexOf(rest, `"`)
	if end < 0 {
		return desc
	}
	return rest[:end]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// collectSamples runs emit fn against a buffered channel, closes it, and drains.
func collectSamples(t *testing.T, emit func(ch chan<- prometheus.Metric)) []sample {
	t.Helper()
	ch := make(chan prometheus.Metric, 512)
	emit(ch)
	close(ch)
	return drain(t, ch)
}

// countByName returns how many samples carry the given fqName.
func countByName(samples []sample, fqName string) int {
	n := 0
	for _, s := range samples {
		if s.fqName == fqName {
			n++
		}
	}
	return n
}

// findOne returns the single sample matching fqName and the given label subset.
// Fails if zero or more than one match (forces tests to pin an exact series).
func findOne(t *testing.T, samples []sample, fqName string, wantLabels map[string]string) sample {
	t.Helper()
	var matches []sample
	for _, s := range samples {
		if s.fqName != fqName {
			continue
		}
		ok := true
		for k, v := range wantLabels {
			if s.labels[k] != v {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, s)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("findOne(%s, %v): want exactly 1 match, got %d", fqName, wantLabels, len(matches))
	}
	return matches[0]
}

// hasName returns true if any sample carries fqName.
func hasName(samples []sample, fqName string) bool {
	return countByName(samples, fqName) > 0
}

// --- shouldRefetch / totalsFromMetric ----------------------------------------

func TestShouldRefetch(t *testing.T) {
	t.Parallel()

	// base metric whose totals exactly match the cached struct below.
	base := &TdarrMetric{
		TotalFileCount:        100,
		TotalTranscodeCount:   40,
		TotalHealthCheckCount: 30,
		HoldQueue:             1,
		TranscodeQueue:        2,
		TranscodeSuccess:      3,
		TranscodeFailed:       4,
		HealthCheckQueue:      5,
		HealthCheckSuccess:    6,
		HealthCheckFailed:     7,
	}
	matchingCache := totalsFromMetric(base)

	// mutate returns a copy of base with one field changed, to prove every field
	// participates in the refetch decision.
	mutate := func(f func(m *TdarrMetric)) *TdarrMetric {
		cp := *base
		f(&cp)
		return &cp
	}

	tests := []struct {
		name        string
		cached      tdarrCacheTotals
		libStatsNil bool
		metric      *TdarrMetric
		want        bool
	}{
		{
			name:        "empty cache (libStatsNil) forces refetch even when totals match",
			cached:      matchingCache,
			libStatsNil: true,
			metric:      base,
			want:        true,
		},
		{
			name:        "totals match and cache populated -> no refetch",
			cached:      matchingCache,
			libStatsNil: false,
			metric:      base,
			want:        false,
		},
		{
			name:        "zero cache vs zero metric -> no refetch (graceful degradation)",
			cached:      tdarrCacheTotals{},
			libStatsNil: false,
			metric:      &TdarrMetric{},
			want:        false,
		},
		{
			name:        "totalFileCount changed -> refetch",
			cached:      matchingCache,
			libStatsNil: false,
			metric:      mutate(func(m *TdarrMetric) { m.TotalFileCount = 101 }),
			want:        true,
		},
		{
			name:        "totalTranscodeCount changed -> refetch",
			cached:      matchingCache,
			libStatsNil: false,
			metric:      mutate(func(m *TdarrMetric) { m.TotalTranscodeCount = 41 }),
			want:        true,
		},
		{
			name:        "totalHealthCheckCount changed -> refetch",
			cached:      matchingCache,
			libStatsNil: false,
			metric:      mutate(func(m *TdarrMetric) { m.TotalHealthCheckCount = 31 }),
			want:        true,
		},
		{
			name:        "table0Count (holdQueue) changed -> refetch",
			cached:      matchingCache,
			libStatsNil: false,
			metric:      mutate(func(m *TdarrMetric) { m.HoldQueue = 99 }),
			want:        true,
		},
		{
			name:        "table1Count (transcodeQueue) changed -> refetch",
			cached:      matchingCache,
			libStatsNil: false,
			metric:      mutate(func(m *TdarrMetric) { m.TranscodeQueue = 99 }),
			want:        true,
		},
		{
			name:        "table6Count (healthCheckFailed) changed -> refetch",
			cached:      matchingCache,
			libStatsNil: false,
			metric:      mutate(func(m *TdarrMetric) { m.HealthCheckFailed = 99 }),
			want:        true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldRefetch(tc.cached, tc.libStatsNil, tc.metric); got != tc.want {
				t.Errorf("shouldRefetch = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTotalsFromMetric(t *testing.T) {
	t.Parallel()
	metric := &TdarrMetric{
		TotalFileCount:        11,
		TotalTranscodeCount:   22,
		TotalHealthCheckCount: 33,
		HoldQueue:             44,
		TranscodeQueue:        55,
		TranscodeSuccess:      66,
		TranscodeFailed:       77,
		HealthCheckQueue:      88,
		HealthCheckSuccess:    99,
		HealthCheckFailed:     110,
	}
	want := tdarrCacheTotals{
		totalFileCount:        11,
		totalTranscodeCount:   22,
		totalHealthCheckCount: 33,
		holdQueue:             44,
		transcodeQueue:        55,
		transcodeSuccess:      66,
		transcodeFailed:       77,
		healthCheckQueue:      88,
		healthCheckSuccess:    99,
		healthCheckFailed:     110,
	}
	if got := totalsFromMetric(metric); got != want {
		t.Errorf("totalsFromMetric mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// --- emitPieSlices / emitPieMetrics ------------------------------------------

func TestEmitPieMetrics(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, newSuccessFakeAPI(cfg))

	pie := &TdarrPieStats{
		libraryName: "Music",
		libraryId:   "lib-audio-01",
		PieStats: TdarrPieStat{
			TotalFiles:            12,
			TotalTranscodeCount:   5,
			TotalHealthCheckCount: 3,
			SizeDiff:              -1.5,
			Video: TdarrPieVideoSlice{
				Codecs:      []TdarrPieSlice{{Name: "H264", Value: 7}},
				Containers:  []TdarrPieSlice{{Name: "MKV", Value: 4}},
				Resolutions: []TdarrPieSlice{{Name: "1080P", Value: 2}},
			},
			Audio: TdarrPieVideoSlice{
				Codecs:     []TdarrPieSlice{{Name: "AAC", Value: 8}, {Name: "FLAC", Value: 1}},
				Containers: []TdarrPieSlice{{Name: "M4A", Value: 6}},
			},
		},
		NormalizedTranscodes:   map[string]int{"success": 5, "queued": 0},
		NormalizedHealthChecks: map[string]int{"success": 3},
	}

	samples := collectSamples(t, func(ch chan<- prometheus.Metric) {
		c.emitPieMetrics(ch, []*TdarrPieStats{pie})
	})

	// totals
	if got := findOne(t, samples, "tdarr_library_files_total",
		map[string]string{"library_name": "Music", "library_id": "lib-audio-01"}).value; got != 12 {
		t.Errorf("files_total = %v, want 12", got)
	}
	if got := findOne(t, samples, "tdarr_library_size_diff_gb",
		map[string]string{"library_id": "lib-audio-01"}).value; got != -1.5 {
		t.Errorf("size_diff = %v, want -1.5", got)
	}

	// normalized status maps emit one series per key
	if n := countByName(samples, "tdarr_library_transcodes"); n != 2 {
		t.Errorf("library_transcodes series count = %d, want 2", n)
	}
	if got := findOne(t, samples, "tdarr_library_transcodes",
		map[string]string{"library_id": "lib-audio-01", "status": "success"}).value; got != 5 {
		t.Errorf("transcodes{success} = %v, want 5", got)
	}
	if got := findOne(t, samples, "tdarr_library_health_checks",
		map[string]string{"library_id": "lib-audio-01", "status": "success"}).value; got != 3 {
		t.Errorf("health_checks{success} = %v, want 3", got)
	}

	// slice labels are lowercased
	if got := findOne(t, samples, "tdarr_library_video_codecs",
		map[string]string{"library_id": "lib-audio-01", "codec": "h264"}).value; got != 7 {
		t.Errorf("video_codecs{h264} = %v, want 7", got)
	}
	if got := findOne(t, samples, "tdarr_library_video_resolutions",
		map[string]string{"library_id": "lib-audio-01", "resolution": "1080p"}).value; got != 2 {
		t.Errorf("video_resolutions{1080p} = %v, want 2", got)
	}
	if got := findOne(t, samples, "tdarr_library_audio_codecs",
		map[string]string{"library_id": "lib-audio-01", "codec": "flac"}).value; got != 1 {
		t.Errorf("audio_codecs{flac} = %v, want 1", got)
	}
	if got := findOne(t, samples, "tdarr_library_audio_containers",
		map[string]string{"library_id": "lib-audio-01", "container_type": "m4a"}).value; got != 6 {
		t.Errorf("audio_containers{m4a} = %v, want 6", got)
	}

	// const instance label is propagated
	s := findOne(t, samples, "tdarr_library_files_total",
		map[string]string{"library_id": "lib-audio-01"})
	if s.labels["tdarr_instance"] != "test-instance" {
		t.Errorf("tdarr_instance label = %q, want test-instance", s.labels["tdarr_instance"])
	}
}

func TestEmitPieSlices_LowercasesNames(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, newSuccessFakeAPI(cfg))

	slices := []TdarrPieSlice{{Name: "HEVC", Value: 3}, {Name: "Av1", Value: 9}}
	samples := collectSamples(t, func(ch chan<- prometheus.Metric) {
		emitPieSlices(ch, c.pieVideoCodecs, "Shows", "lib-video-01", slices)
	})

	if len(samples) != 2 {
		t.Fatalf("want 2 samples, got %d", len(samples))
	}
	var gotNames []string
	for _, s := range samples {
		gotNames = append(gotNames, s.labels["codec"])
		if s.labels["library_name"] != "Shows" || s.labels["library_id"] != "lib-video-01" {
			t.Errorf("unexpected library labels: %v", s.labels)
		}
	}
	sort.Strings(gotNames)
	if gotNames[0] != "av1" || gotNames[1] != "hevc" {
		t.Errorf("codec labels = %v, want lowercased [av1 hevc]", gotNames)
	}
	if got := findOne(t, samples, "tdarr_library_video_codecs",
		map[string]string{"codec": "hevc"}).value; got != 3 {
		t.Errorf("value for hevc = %v, want 3", got)
	}
}

// --- emitNodeMetrics ----------------------------------------------------------

func TestEmitNodeMetrics(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, newSuccessFakeAPI(cfg))

	busy := TdarrNode{
		Id:   "node-busy",
		Name: "BusyNode",
		ResourceStats: TdarrResourceStats{
			Process: struct {
				Uptime      int64  `json:"uptime"`
				HeapUsedMb  string `json:"heapUsedMB"`
				HeapTotalMb string `json:"heapTotalMB"`
			}{Uptime: 3600, HeapUsedMb: "128.5", HeapTotalMb: "256.0"},
			Os: struct {
				CpuPercent string `json:"cpuPerc"`
				MemUsedGb  string `json:"memUsedGB"`
				MemTotalGb string `json:"memTotalGB"`
			}{CpuPercent: "42.0", MemUsedGb: "8.0", MemTotalGb: "16.0"},
		},
		Workers: map[string]TdarrNodeWorkers{
			"w1": {
				Id:                 "w1",
				WorkerType:         "transcodecpu",
				Percentage:         55.5,
				Fps:                24,
				OriginalfileSizeGb: 2.0,
				OutputFileSizeGb:   1.0,
				EstSizeGb:          1.2,
				Status:             "processing",
				File:               "movie.mkv",
				Eta:                "0:30:00",
			},
		},
	}

	idle := TdarrNode{
		Id:   "node-idle",
		Name: "IdleNode",
		ResourceStats: TdarrResourceStats{
			Process: struct {
				Uptime      int64  `json:"uptime"`
				HeapUsedMb  string `json:"heapUsedMB"`
				HeapTotalMb string `json:"heapTotalMB"`
			}{Uptime: 10, HeapUsedMb: "not-a-number", HeapTotalMb: "64.0"},
			Os: struct {
				CpuPercent string `json:"cpuPerc"`
				MemUsedGb  string `json:"memUsedGB"`
				MemTotalGb string `json:"memTotalGB"`
			}{CpuPercent: "1.0", MemUsedGb: "1.0", MemTotalGb: "8.0"},
		},
		Workers: map[string]TdarrNodeWorkers{},
	}

	nodeData := map[string]TdarrNode{"node-busy": busy, "node-idle": idle}
	samples := collectSamples(t, func(ch chan<- prometheus.Metric) {
		c.emitNodeMetrics(ch, nodeData)
	})

	busyL := map[string]string{"node_id": "node-busy"}
	idleL := map[string]string{"node_id": "node-idle"}

	// node identity emitted once per node
	if n := countByName(samples, "tdarr_node_info"); n != 2 {
		t.Errorf("node_info count = %d, want 2", n)
	}

	// resource stats: busy node parses all; idle node's heap-used is unparseable -> absent.
	if got := findOne(t, samples, "tdarr_node_heap_used_mb", busyL).value; got != 128.5 {
		t.Errorf("busy heap_used = %v, want 128.5", got)
	}
	// idle node heap-used must be skipped (unparseable), but heap-total (parseable) present.
	for _, s := range samples {
		if s.fqName == "tdarr_node_heap_used_mb" && s.labels["node_id"] == "node-idle" {
			t.Errorf("idle node heap_used_mb should be skipped (unparseable), but was emitted: %v", s.value)
		}
	}
	if got := findOne(t, samples, "tdarr_node_heap_total_mb", idleL).value; got != 64.0 {
		t.Errorf("idle heap_total = %v, want 64.0", got)
	}
	if got := findOne(t, samples, "tdarr_node_host_cpu_percent", busyL).value; got != 42.0 {
		t.Errorf("busy cpu_percent = %v, want 42.0", got)
	}

	// per-type worker_count: four known dims always emitted per node (zeros included).
	if got := findOne(t, samples, "tdarr_node_worker_count",
		map[string]string{"node_id": "node-busy", "worker_type": "transcode", "compute_type": "cpu"}).value; got != 1 {
		t.Errorf("busy worker_count{transcode,cpu} = %v, want 1", got)
	}
	if got := findOne(t, samples, "tdarr_node_worker_count",
		map[string]string{"node_id": "node-busy", "worker_type": "healthcheck", "compute_type": "gpu"}).value; got != 0 {
		t.Errorf("busy worker_count{healthcheck,gpu} = %v, want 0", got)
	}
	if n := countByName(samples, "tdarr_node_worker_count"); n != 8 {
		// 4 known dims * 2 nodes, no unknown buckets.
		t.Errorf("worker_count series total = %d, want 8", n)
	}

	// per-worker gauges only for the busy node's worker.
	if got := findOne(t, samples, "tdarr_node_worker_percentage",
		map[string]string{"node_id": "node-busy", "worker_id": "w1"}).value; got != 55.5 {
		t.Errorf("worker percentage = %v, want 55.5", got)
	}
	if got := findOne(t, samples, "tdarr_node_worker_fps",
		map[string]string{"worker_id": "w1"}).value; got != 24 {
		t.Errorf("worker fps = %v, want 24", got)
	}
	// ETA "0:30:00" -> 1800 seconds.
	if got := findOne(t, samples, "tdarr_node_worker_eta_seconds",
		map[string]string{"worker_id": "w1"}).value; got != 1800 {
		t.Errorf("worker eta_seconds = %v, want 1800", got)
	}
	// idle node has no workers -> no per-worker series.
	for _, fq := range []string{"tdarr_node_worker_percentage", "tdarr_node_worker_info"} {
		for _, s := range samples {
			if s.fqName == fq && s.labels["node_id"] == "node-idle" {
				t.Errorf("idle node should emit no %s, but got one", fq)
			}
		}
	}

	// node_info / worker_info label correctness for the busy worker.
	wi := findOne(t, samples, "tdarr_node_worker_info",
		map[string]string{"node_id": "node-busy", "worker_id": "w1"})
	if wi.labels["worker_type"] != "transcode" || wi.labels["compute_type"] != "cpu" {
		t.Errorf("worker_info type labels = %v, want transcode/cpu", wi.labels)
	}
	if wi.labels["worker_file"] != "movie.mkv" {
		t.Errorf("worker_info file label = %v, want movie.mkv", wi.labels)
	}
	// worker_status is no longer a label on worker_info; it moved to the dedicated status metric.
	if _, ok := wi.labels["worker_status"]; ok {
		t.Errorf("worker_info should not carry worker_status label (moved to tdarr_node_worker_status)")
	}
	// verify the dedicated worker_status metric carries the status label.
	ws := findOne(t, samples, "tdarr_node_worker_status",
		map[string]string{"node_id": "node-busy", "worker_id": "w1"})
	if ws.labels["worker_status"] != "processing" {
		t.Errorf("worker_status metric status label = %q, want %q", ws.labels["worker_status"], "processing")
	}

	// sanity: every node emits worker_limit and queue_length for the four known dims.
	if !hasName(samples, "tdarr_node_worker_limit") || !hasName(samples, "tdarr_node_queue_length") {
		t.Error("expected per-type worker_limit and queue_length series")
	}
}

// --- emitGeneralMetrics -------------------------------------------------------

func TestEmitGeneralMetrics(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	c := newTdarrCollectorWithAPI(cfg, newSuccessFakeAPI(cfg))

	metric := &TdarrMetric{
		TotalFileCount:        100,
		TotalTranscodeCount:   40,
		TotalHealthCheckCount: 30,
		SizeDiff:              12.5,
		AvgNumStreams:         2.0,
		StreamStats: TdarrStreamStats{
			Duration:  TdarrStreamStatsObj{Average: 1, Highest: 2, Total: 3},
			BitRate:   TdarrStreamStatsObj{Average: 4, Highest: 5, Total: 6},
			NumFrames: TdarrStreamStatsObj{Average: 7, Highest: 8, Total: 9},
		},
	}
	samples := collectSamples(t, func(ch chan<- prometheus.Metric) {
		c.emitGeneralMetrics(ch, metric, 88.0, 77.0)
	})

	if got := findOne(t, samples, "tdarr_files_total", map[string]string{}).value; got != 100 {
		t.Errorf("files_total = %v, want 100", got)
	}
	if got := findOne(t, samples, "tdarr_score_pct", map[string]string{}).value; got != 88.0 {
		t.Errorf("score_pct = %v, want 88.0", got)
	}
	if got := findOne(t, samples, "tdarr_health_check_score_pct", map[string]string{}).value; got != 77.0 {
		t.Errorf("health_check_score_pct = %v, want 77.0", got)
	}
	if got := findOne(t, samples, "tdarr_size_diff_gb", map[string]string{}).value; got != 12.5 {
		t.Errorf("size_diff_gb = %v, want 12.5", got)
	}
	// stream stats: 3 stat_type series each for duration/bit_rate/num_frames.
	if n := countByName(samples, "tdarr_stream_stats_duration"); n != 3 {
		t.Errorf("stream_stats_duration series = %d, want 3", n)
	}
	if got := findOne(t, samples, "tdarr_stream_stats_bit_rate",
		map[string]string{"stat_type": "total"}).value; got != 6 {
		t.Errorf("bit_rate{total} = %v, want 6", got)
	}
	if got := findOne(t, samples, "tdarr_stream_stats_num_frames",
		map[string]string{"stat_type": "average"}).value; got != 7 {
		t.Errorf("num_frames{average} = %v, want 7", got)
	}
}
