package collector

import (
	"fmt"

	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

// Known worker type / compute type label values used for per-type metric emission.
// The two dimensions are kept as separate labels so Prometheus aggregations can
// project either axis cleanly (e.g. sum by (worker_type) or sum by (compute_type)).
const (
	workerTypeTranscode   = "transcode"
	workerTypeHealthCheck = "healthcheck"
	computeTypeCpu        = "cpu"
	computeTypeGpu        = "gpu"
	// computeTypeUnknown is the compute_type sentinel emitted when parseWorkerType
	// cannot map Tdarr's compound API string to one of the four known compounds.
	computeTypeUnknown = "unknown"
)

// workerTypeDim is one (worker_type, compute_type) coordinate emitted for
// per-type gauges so zero-value series appear even when no workers are active.
type workerTypeDim struct {
	workerType  string
	computeType string
}

// knownWorkerTypeDims is the ordered list of all known (worker_type, compute_type)
// pairs emitted for per-type gauges (worker_count, worker_limit, queue_length).
var knownWorkerTypeDims = []workerTypeDim{
	{workerTypeTranscode, computeTypeCpu},
	{workerTypeTranscode, computeTypeGpu},
	{workerTypeHealthCheck, computeTypeCpu},
	{workerTypeHealthCheck, computeTypeGpu},
}

// parseWorkerType splits Tdarr's compound worker-type string into the two
// label dimensions. Returns the raw input as worker_type and computeTypeUnknown
// for compute_type when the value is not one of the four known compounds.
func parseWorkerType(api string) (workerType, computeType string) {
	switch api {
	case "transcodecpu":
		return workerTypeTranscode, computeTypeCpu
	case "transcodegpu":
		return workerTypeTranscode, computeTypeGpu
	case "healthcheckcpu":
		return workerTypeHealthCheck, computeTypeCpu
	case "healthcheckgpu":
		return workerTypeHealthCheck, computeTypeGpu
	}
	return api, computeTypeUnknown
}

type TdarrNodeMetrics struct {
	// identity / info
	nodeInfo typedDesc
	// resource stats
	nodeUptime         typedDesc
	nodeHeapUsedMb     typedDesc
	nodeHeapTotalMb    typedDesc
	nodeHostCpuPercent typedDesc
	nodeHostMemUsedGb  typedDesc
	nodeHostMemTotalGb typedDesc
	// node state gauges
	nodePaused          typedDesc
	nodeMaxGpuWorkers   typedDesc
	nodeScheduleEnabled typedDesc
	// per-type node gauges; split across two labels:
	//   worker_type  ∈ {transcode, healthcheck}
	//   compute_type ∈ {cpu, gpu}
	// Unknown API values from Tdarr emit the raw string as worker_type with compute_type="unknown".
	nodeWorkerCount typedDesc
	nodeWorkerLimit typedDesc
	nodeQueueLength typedDesc
	// worker identity / info
	nodeWorkerInfo typedDesc
	// per-worker numeric gauges
	nodeWorkerPercentage         typedDesc
	nodeWorkerFps                typedDesc
	nodeWorkerOriginalFileSizeGb typedDesc
	nodeWorkerOutputFileSizeGb   typedDesc
	nodeWorkerEstFileSizeGb      typedDesc
	nodeWorkerJobStartTimestamp  typedDesc
	nodeWorkerStartTimestamp     typedDesc
	nodeWorkerStatusTimestamp    typedDesc
	nodeWorkerEtaSeconds         typedDesc
	nodeWorkerPid                typedDesc
}

type TdarrNodeCollector struct {
	// Only the node API path is read at collect time; the instance label is
	// baked into the descs at construction, so the full config bag is not stored.
	nodePath string
	api      tdarrAPI // shared with the parent TdarrCollector (same base URL)
	metrics  *TdarrNodeMetrics
}

func NewTdarrNodeMetrics(runConfig config.Config) *TdarrNodeMetrics {
	nodeLabelPair := []string{"node_id", "node_name"}
	nodeTypeLabelPair := []string{"node_id", "node_name", "worker_type", "compute_type"}
	workerLabelPair := []string{"node_id", "node_name", "worker_id"}
	instance := prometheus.Labels{"tdarr_instance": runConfig.InstanceName}

	return &TdarrNodeMetrics{
		nodeInfo: newGauge(
			"node_info",
			"Tdarr node identity information",
			[]string{"node_id", "node_name", "gpu_select", "node_pid", "node_priority",
				"gpu_can_do_cpu"},
			instance,
		),
		nodeUptime: newGauge(
			"node_uptime_seconds",
			"Tdarr node uptime in seconds",
			nodeLabelPair, instance,
		),
		nodeHeapUsedMb: newGauge(
			"node_heap_used_mb",
			"Tdarr node heap used in MB",
			nodeLabelPair, instance,
		),
		nodeHeapTotalMb: newGauge(
			"node_heap_total_mb",
			"Tdarr node heap total in MB",
			nodeLabelPair, instance,
		),
		nodeHostCpuPercent: newGauge(
			"node_host_cpu_percent",
			"Tdarr node cpu percent used",
			nodeLabelPair, instance,
		),
		nodeHostMemUsedGb: newGauge(
			"node_host_mem_used_gb",
			"Memory used in GB for host that Tdarr node is running on",
			nodeLabelPair, instance,
		),
		nodeHostMemTotalGb: newGauge(
			"node_host_mem_total_gb",
			"Total memory in GB for host that Tdarr node is running on",
			nodeLabelPair, instance,
		),
		nodePaused: newGauge(
			"node_paused",
			"1 if the Tdarr node is paused, 0 otherwise",
			nodeLabelPair, instance,
		),
		nodeMaxGpuWorkers: newGauge(
			"node_max_gpu_workers",
			"Maximum number of GPU workers configured for the Tdarr node",
			nodeLabelPair, instance,
		),
		nodeScheduleEnabled: newGauge(
			"node_schedule_enabled",
			"1 if scheduled operation is enabled on the Tdarr node, 0 otherwise",
			nodeLabelPair, instance,
		),
		nodeWorkerCount: newGauge(
			"node_worker_count",
			"Number of active workers on the Tdarr node by worker_type and compute_type",
			nodeTypeLabelPair, instance,
		),
		nodeWorkerLimit: newGauge(
			"node_worker_limit",
			"Configured worker limit on the Tdarr node by worker_type and compute_type",
			nodeTypeLabelPair, instance,
		),
		nodeQueueLength: newGauge(
			"node_queue_length",
			"Current queue length on the Tdarr node by worker_type and compute_type",
			nodeTypeLabelPair, instance,
		),
		nodeWorkerInfo: newGauge(
			"node_worker_info",
			"Tdarr node worker identity and categorical state (always 1)",
			[]string{"node_id", "node_name", "worker_id", "worker_type", "compute_type", "flow_worker",
				"worker_status", "worker_file",
				"worker_plugin_id", "worker_plugin_position",
				"worker_connected", "worker_idle"},
			instance,
		),
		nodeWorkerPercentage: newGauge(
			"node_worker_percentage",
			"Tdarr node worker transcode/healthcheck progress percentage",
			workerLabelPair, instance,
		),
		nodeWorkerFps: newGauge(
			"node_worker_fps",
			"Tdarr node worker frames per second",
			workerLabelPair, instance,
		),
		nodeWorkerOriginalFileSizeGb: newGauge(
			"node_worker_original_file_size_gb",
			"Tdarr node worker original file size in GB",
			workerLabelPair, instance,
		),
		nodeWorkerOutputFileSizeGb: newGauge(
			"node_worker_output_file_size_gb",
			"Tdarr node worker current output file size in GB",
			workerLabelPair, instance,
		),
		nodeWorkerEstFileSizeGb: newGauge(
			"node_worker_est_file_size_gb",
			"Tdarr node worker estimated output file size in GB",
			workerLabelPair, instance,
		),
		nodeWorkerJobStartTimestamp: newGauge(
			"node_worker_job_start_timestamp_seconds",
			"Tdarr node worker job start time as Unix timestamp in seconds",
			workerLabelPair, instance,
		),
		nodeWorkerStartTimestamp: newGauge(
			"node_worker_start_timestamp_seconds",
			"Tdarr node worker current plugin step start time as Unix timestamp in seconds",
			workerLabelPair, instance,
		),
		nodeWorkerStatusTimestamp: newGauge(
			"node_worker_status_timestamp_seconds",
			"Tdarr node worker last status update time as Unix timestamp in seconds",
			workerLabelPair, instance,
		),
		nodeWorkerEtaSeconds: newGauge(
			"node_worker_eta_seconds",
			"Tdarr node worker estimated time remaining in seconds",
			workerLabelPair, instance,
		),
		nodeWorkerPid: newGauge(
			"node_worker_pid",
			"Tdarr node worker process ID",
			workerLabelPair, instance,
		),
	}
}

// descs returns the node metrics' descs in Describe order. The TdarrCollector's Describe
// appends this to its own descs so it never reaches into TdarrNodeMetrics field-by-field —
// the node metric set is owned and ordered here, in one place.
func (m *TdarrNodeMetrics) descs() []typedDesc {
	return []typedDesc{
		m.nodeInfo,
		m.nodeUptime,
		m.nodeHeapUsedMb,
		m.nodeHeapTotalMb,
		m.nodeHostCpuPercent,
		m.nodeHostMemUsedGb,
		m.nodeHostMemTotalGb,
		m.nodePaused,
		m.nodeMaxGpuWorkers,
		m.nodeScheduleEnabled,
		m.nodeWorkerCount,
		m.nodeWorkerLimit,
		m.nodeQueueLength,
		m.nodeWorkerInfo,
		m.nodeWorkerPercentage,
		m.nodeWorkerFps,
		m.nodeWorkerOriginalFileSizeGb,
		m.nodeWorkerOutputFileSizeGb,
		m.nodeWorkerEstFileSizeGb,
		m.nodeWorkerJobStartTimestamp,
		m.nodeWorkerStartTimestamp,
		m.nodeWorkerStatusTimestamp,
		m.nodeWorkerEtaSeconds,
		m.nodeWorkerPid,
	}
}

// NewTdarrNodeCollector wires the shared tdarrAPI (built by the parent collector)
// into the node collector so node requests reuse the same HTTP client.
func NewTdarrNodeCollector(runConfig config.Config, api tdarrAPI) *TdarrNodeCollector {
	return &TdarrNodeCollector{
		nodePath: runConfig.TdarrNodePath,
		api:      api,
		metrics:  NewTdarrNodeMetrics(runConfig),
	}
}

func (n *TdarrNodeCollector) GetNodeData() (map[string]TdarrNode, error) {
	// get node data
	nodeData := map[string]TdarrNode{}
	nodeHttpErr := n.api.DoRequest(n.nodePath, &nodeData)
	if nodeHttpErr != nil {
		return nil, fmt.Errorf("get node data: %w: %w", ErrUpstream, nodeHttpErr)
	}
	log.Debug().Interface("response", nodeData).Msg("Node Api Response")
	return nodeData, nil
}

// emitPerType emits a gauge metric for all four known (worker_type, compute_type)
// dimensions using values from the provided TdarrNodeJobs struct. This ensures
// zero-value series are always emitted even when no workers of a given type are active.
func emitPerType(ch chan<- prometheus.Metric, desc typedDesc, nodeId, nodeName string, jobs TdarrNodeJobs) {
	ch <- desc.mustNewConstMetric(float64(jobs.TranscodeCpu), nodeId, nodeName, workerTypeTranscode, computeTypeCpu)
	ch <- desc.mustNewConstMetric(float64(jobs.TranscodeGpu), nodeId, nodeName, workerTypeTranscode, computeTypeGpu)
	ch <- desc.mustNewConstMetric(float64(jobs.HealthCheckCpu), nodeId, nodeName, workerTypeHealthCheck, computeTypeCpu)
	ch <- desc.mustNewConstMetric(float64(jobs.HealthCheckGpu), nodeId, nodeName, workerTypeHealthCheck, computeTypeGpu)
}

// workerCountResult is the per-dim aggregate returned by  countWorkersByType.
// known holds counts for the four canonical dims (always present, zero allowed
// for zero-emission). unknown holds counts keyed by the raw API string Tdarr
// emitted, so two distinct unknown strings produce two distinct series instead
// of being collapsed.
type workerCountResult struct {
	known   map[workerTypeDim]int
	unknown map[string]int // raw API string -> count
}

// countWorkersByType counts active workers in the provided workers map grouped
// by their WorkerType field (parsed into worker_type + compute_type). Unknown
// API strings are bucketed by raw value so caller can emit (raw, "unknown", count)
// series. A warning is logged for each occurrence of an unknown type.
func countWorkersByType(workers map[string]TdarrNodeWorkers) workerCountResult {
	result := workerCountResult{
		known:   make(map[workerTypeDim]int, len(knownWorkerTypeDims)),
		unknown: map[string]int{},
	}
	for _, d := range knownWorkerTypeDims {
		result.known[d] = 0
	}
	for _, w := range workers {
		wt, ct := parseWorkerType(w.WorkerType)
		if ct == computeTypeUnknown {
			log.Warn().Str("workerType", w.WorkerType).Msg("Unknown worker type encountered; bucketing under 'unknown'")
			result.unknown[w.WorkerType]++
			continue
		}
		result.known[workerTypeDim{wt, ct}]++
	}
	return result
}

// parseEtaSeconds converts Tdarr's "H:MM:SS" ETA string to integer seconds.
// Returns (0, false) on parse failure; caller should skip emitting the metric.
func parseEtaSeconds(eta string) (int64, bool) {
	// Tdarr format: "H:MM:SS" (hours may be > 1 digit)
	var h, m, s int64
	n, err := fmt.Sscanf(eta, "%d:%d:%d", &h, &m, &s)
	if err != nil || n != 3 {
		return 0, false
	}
	return h*3600 + m*60 + s, true
}
