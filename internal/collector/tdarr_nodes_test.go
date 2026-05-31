package collector

import (
	"sort"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// ---------------------------------------------------------------------------
// parseEtaSeconds
// ---------------------------------------------------------------------------

func TestParseEtaSeconds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantSecs  int64
		wantValid bool
	}{
		{
			name:      "zero eta",
			input:     "0:00:00",
			wantSecs:  0,
			wantValid: true,
		},
		{
			name:      "single-digit hour",
			input:     "1:02:03",
			wantSecs:  1*3600 + 2*60 + 3,
			wantValid: true,
		},
		{
			name:      "multi-digit hours",
			input:     "12:34:56",
			wantSecs:  12*3600 + 34*60 + 56,
			wantValid: true,
		},
		{
			name:      "large hours value",
			input:     "100:00:00",
			wantSecs:  100 * 3600,
			wantValid: true,
		},
		{
			name:      "only minutes and seconds without hours separator",
			input:     "30:45",
			wantSecs:  0,
			wantValid: false,
		},
		{
			name:      "empty string",
			input:     "",
			wantSecs:  0,
			wantValid: false,
		},
		{
			name:      "non-numeric",
			input:     "N/A",
			wantSecs:  0,
			wantValid: false,
		},
		{
			name:      "too many segments",
			input:     "1:2:3:4",
			// fmt.Sscanf reads exactly three integers and succeeds; the trailing ":4" is ignored.
			// Actual behavior: n==3, err==nil -> returns (1*3600+2*60+3, true).
			wantSecs:  1*3600 + 2*60 + 3,
			wantValid: true,
		},
		{
			name:      "partial input missing last segment",
			input:     "1:02",
			wantSecs:  0,
			wantValid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseEtaSeconds(tc.input)
			if ok != tc.wantValid {
				t.Errorf("parseEtaSeconds(%q) valid=%v, want %v", tc.input, ok, tc.wantValid)
			}
			if got != tc.wantSecs {
				t.Errorf("parseEtaSeconds(%q) seconds=%d, want %d", tc.input, got, tc.wantSecs)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseWorkerType
// ---------------------------------------------------------------------------

func TestParseWorkerType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           string
		wantWorkerType  string
		wantComputeType string
	}{
		{
			name:            "transcodecpu",
			input:           "transcodecpu",
			wantWorkerType:  workerTypeTranscode,
			wantComputeType: computeTypeCpu,
		},
		{
			name:            "transcodegpu",
			input:           "transcodegpu",
			wantWorkerType:  workerTypeTranscode,
			wantComputeType: computeTypeGpu,
		},
		{
			name:            "healthcheckcpu",
			input:           "healthcheckcpu",
			wantWorkerType:  workerTypeHealthCheck,
			wantComputeType: computeTypeCpu,
		},
		{
			name:            "healthcheckgpu",
			input:           "healthcheckgpu",
			wantWorkerType:  workerTypeHealthCheck,
			wantComputeType: computeTypeGpu,
		},
		{
			name:            "unknown preserves raw string as worker_type",
			input:           "somefuturetype",
			wantWorkerType:  "somefuturetype",
			wantComputeType: computeTypeUnknown,
		},
		{
			name:            "empty string is unknown",
			input:           "",
			wantWorkerType:  "",
			wantComputeType: computeTypeUnknown,
		},
		{
			name:            "case-sensitive: uppercase is unknown",
			input:           "TranscodeCpu",
			wantWorkerType:  "TranscodeCpu",
			wantComputeType: computeTypeUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wt, ct := parseWorkerType(tc.input)
			if wt != tc.wantWorkerType {
				t.Errorf("parseWorkerType(%q) workerType=%q, want %q", tc.input, wt, tc.wantWorkerType)
			}
			if ct != tc.wantComputeType {
				t.Errorf("parseWorkerType(%q) computeType=%q, want %q", tc.input, ct, tc.wantComputeType)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// countWorkersByType
// ---------------------------------------------------------------------------

func TestCountWorkersByType(t *testing.T) {
	t.Parallel()

	t.Run("empty workers map returns all four known dims at zero", func(t *testing.T) {
		t.Parallel()
		result := countWorkersByType(map[string]TdarrNodeWorkers{})

		// All four known dims must be present with value 0.
		for _, dim := range knownWorkerTypeDims {
			got, ok := result.known[dim]
			if !ok {
				t.Errorf("dim {%s,%s} missing from known map", dim.workerType, dim.computeType)
				continue
			}
			if got != 0 {
				t.Errorf("dim {%s,%s}: want 0, got %d", dim.workerType, dim.computeType, got)
			}
		}
		if len(result.unknown) != 0 {
			t.Errorf("unknown map: want empty, got %v", result.unknown)
		}
	})

	t.Run("known worker types are counted per dim", func(t *testing.T) {
		t.Parallel()
		workers := map[string]TdarrNodeWorkers{
			"w1": {WorkerType: "transcodecpu"},
			"w2": {WorkerType: "transcodecpu"},
			"w3": {WorkerType: "transcodegpu"},
			"w4": {WorkerType: "healthcheckcpu"},
			// healthcheckgpu intentionally absent → must still appear as 0.
		}
		result := countWorkersByType(workers)

		want := map[workerTypeDim]int{
			{workerTypeTranscode, computeTypeCpu}:   2,
			{workerTypeTranscode, computeTypeGpu}:   1,
			{workerTypeHealthCheck, computeTypeCpu}: 1,
			{workerTypeHealthCheck, computeTypeGpu}: 0,
		}
		for dim, wantCount := range want {
			got, ok := result.known[dim]
			if !ok {
				t.Errorf("dim {%s,%s} missing from known map", dim.workerType, dim.computeType)
				continue
			}
			if got != wantCount {
				t.Errorf("dim {%s,%s}: want %d, got %d", dim.workerType, dim.computeType, wantCount, got)
			}
		}
		if len(result.unknown) != 0 {
			t.Errorf("unknown map: want empty, got %v", result.unknown)
		}
	})

	t.Run("unknown worker types bucket by raw API string", func(t *testing.T) {
		t.Parallel()
		workers := map[string]TdarrNodeWorkers{
			"w1": {WorkerType: "transcodecpu"},
			"w2": {WorkerType: "futuretype"},
			"w3": {WorkerType: "futuretype"},
			"w4": {WorkerType: "anothertype"},
		}
		result := countWorkersByType(workers)

		// Known dim for transcodecpu.
		gotCpu := result.known[workerTypeDim{workerTypeTranscode, computeTypeCpu}]
		if gotCpu != 1 {
			t.Errorf("transcodecpu count: want 1, got %d", gotCpu)
		}

		// Unknown bucket: raw strings preserved, not collapsed.
		if result.unknown["futuretype"] != 2 {
			t.Errorf("unknown[futuretype]: want 2, got %d", result.unknown["futuretype"])
		}
		if result.unknown["anothertype"] != 1 {
			t.Errorf("unknown[anothertype]: want 1, got %d", result.unknown["anothertype"])
		}
		// Verify unknown bucket does NOT lump distinct strings together.
		if len(result.unknown) != 2 {
			t.Errorf("unknown bucket length: want 2 distinct keys, got %d: %v", len(result.unknown), result.unknown)
		}
	})

	t.Run("all workers unknown, known dims all zero", func(t *testing.T) {
		t.Parallel()
		workers := map[string]TdarrNodeWorkers{
			"w1": {WorkerType: "mystery"},
		}
		result := countWorkersByType(workers)

		for _, dim := range knownWorkerTypeDims {
			got := result.known[dim]
			if got != 0 {
				t.Errorf("dim {%s,%s}: want 0, got %d", dim.workerType, dim.computeType, got)
			}
		}
		if result.unknown["mystery"] != 1 {
			t.Errorf("unknown[mystery]: want 1, got %d", result.unknown["mystery"])
		}
	})
}

// ---------------------------------------------------------------------------
// emitPerType
// ---------------------------------------------------------------------------

// drainMetricChannel reads all metrics from ch (non-blocking after ch is closed
// or all items have been sent) and returns them as a slice.
func drainMetricChannel(ch <-chan prometheus.Metric) []prometheus.Metric {
	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}
	return metrics
}

// readMetric extracts the dto.Metric from a prometheus.Metric.
func readMetric(t *testing.T, m prometheus.Metric) *dto.Metric {
	t.Helper()
	d := &dto.Metric{}
	if err := m.Write(d); err != nil {
		t.Fatalf("Write metric: %v", err)
	}
	return d
}

// labelValue returns the value of the named label from a dto.Metric, or "" if not found.
func labelValue(d *dto.Metric, name string) string {
	for _, lp := range d.GetLabel() {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
}

func TestEmitPerType(t *testing.T) {
	t.Parallel()

	desc := prometheus.NewDesc(
		"test_node_worker_count",
		"test gauge for emitPerType",
		[]string{"node_id", "node_name", "worker_type", "compute_type"},
		nil,
	)

	t.Run("emits exactly four metrics", func(t *testing.T) {
		t.Parallel()
		ch := make(chan prometheus.Metric, 10)
		jobs := TdarrNodeJobs{
			TranscodeCpu:   1,
			TranscodeGpu:   2,
			HealthCheckCpu: 3,
			HealthCheckGpu: 4,
		}
		emitPerType(ch, desc, "node-1", "mynode", jobs)
		close(ch)

		metrics := drainMetricChannel(ch)
		if len(metrics) != 4 {
			t.Fatalf("emitPerType: want 4 metrics, got %d", len(metrics))
		}
	})

	t.Run("emitted metrics have correct label values and numeric values", func(t *testing.T) {
		t.Parallel()
		ch := make(chan prometheus.Metric, 10)
		jobs := TdarrNodeJobs{
			TranscodeCpu:   1,
			TranscodeGpu:   2,
			HealthCheckCpu: 3,
			HealthCheckGpu: 4,
		}
		emitPerType(ch, desc, "node-42", "testnode", jobs)
		close(ch)

		// Collect and index by (worker_type, compute_type).
		type dimKey struct{ wt, ct string }
		byDim := make(map[dimKey]float64)
		for _, m := range drainMetricChannel(ch) {
			d := readMetric(t, m)
			wt := labelValue(d, "worker_type")
			ct := labelValue(d, "compute_type")
			byDim[dimKey{wt, ct}] = d.GetGauge().GetValue()

			// Verify node labels.
			if got := labelValue(d, "node_id"); got != "node-42" {
				t.Errorf("node_id: want node-42, got %q", got)
			}
			if got := labelValue(d, "node_name"); got != "testnode" {
				t.Errorf("node_name: want testnode, got %q", got)
			}
		}

		want := map[dimKey]float64{
			{workerTypeTranscode, computeTypeCpu}:   1,
			{workerTypeTranscode, computeTypeGpu}:   2,
			{workerTypeHealthCheck, computeTypeCpu}: 3,
			{workerTypeHealthCheck, computeTypeGpu}: 4,
		}
		for k, wantVal := range want {
			got, ok := byDim[k]
			if !ok {
				t.Errorf("dim {%s,%s} not emitted", k.wt, k.ct)
				continue
			}
			if got != wantVal {
				t.Errorf("dim {%s,%s}: want %.0f, got %.0f", k.wt, k.ct, wantVal, got)
			}
		}
	})

	t.Run("zero-value series are emitted for all four dims", func(t *testing.T) {
		t.Parallel()
		ch := make(chan prometheus.Metric, 10)
		// All fields zero — emitPerType must still emit four series.
		jobs := TdarrNodeJobs{}
		emitPerType(ch, desc, "node-0", "zeronode", jobs)
		close(ch)

		type dimKey struct{ wt, ct string }
		seen := make(map[dimKey]float64)
		for _, m := range drainMetricChannel(ch) {
			d := readMetric(t, m)
			wt := labelValue(d, "worker_type")
			ct := labelValue(d, "compute_type")
			seen[dimKey{wt, ct}] = d.GetGauge().GetValue()
		}

		expectedDims := []dimKey{
			{workerTypeTranscode, computeTypeCpu},
			{workerTypeTranscode, computeTypeGpu},
			{workerTypeHealthCheck, computeTypeCpu},
			{workerTypeHealthCheck, computeTypeGpu},
		}
		for _, dim := range expectedDims {
			val, ok := seen[dim]
			if !ok {
				t.Errorf("dim {%s,%s} not emitted for zero-value jobs", dim.wt, dim.ct)
				continue
			}
			if val != 0 {
				t.Errorf("dim {%s,%s}: want 0, got %v", dim.wt, dim.ct, val)
			}
		}
		if len(seen) != 4 {
			t.Errorf("want exactly 4 dims, got %d: %v", len(seen), seen)
		}
	})

	t.Run("emitted metric dim order covers all four known dims exactly", func(t *testing.T) {
		t.Parallel()
		ch := make(chan prometheus.Metric, 10)
		jobs := TdarrNodeJobs{TranscodeCpu: 5, TranscodeGpu: 6, HealthCheckCpu: 7, HealthCheckGpu: 8}
		emitPerType(ch, desc, "n1", "nn", jobs)
		close(ch)

		type dimKey struct{ wt, ct string }
		var dims []dimKey
		for _, m := range drainMetricChannel(ch) {
			d := readMetric(t, m)
			dims = append(dims, dimKey{labelValue(d, "worker_type"), labelValue(d, "compute_type")})
		}
		// Sort both expected and actual for a stable comparison (avoids ordering assumptions).
		expectedDims := []dimKey{
			{workerTypeTranscode, computeTypeCpu},
			{workerTypeTranscode, computeTypeGpu},
			{workerTypeHealthCheck, computeTypeCpu},
			{workerTypeHealthCheck, computeTypeGpu},
		}
		sort.Slice(dims, func(i, j int) bool {
			if dims[i].wt != dims[j].wt {
				return dims[i].wt < dims[j].wt
			}
			return dims[i].ct < dims[j].ct
		})
		sort.Slice(expectedDims, func(i, j int) bool {
			if expectedDims[i].wt != expectedDims[j].wt {
				return expectedDims[i].wt < expectedDims[j].wt
			}
			return expectedDims[i].ct < expectedDims[j].ct
		})
		if len(dims) != len(expectedDims) {
			t.Fatalf("dim count: want %d, got %d", len(expectedDims), len(dims))
		}
		for i := range dims {
			if dims[i] != expectedDims[i] {
				t.Errorf("dim[%d]: want {%s,%s}, got {%s,%s}", i,
					expectedDims[i].wt, expectedDims[i].ct, dims[i].wt, dims[i].ct)
			}
		}
	})
}
