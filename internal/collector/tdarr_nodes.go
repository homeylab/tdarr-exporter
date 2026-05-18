package collector

import (
	"fmt"

	"github.com/homeylab/tdarr-exporter/internal/client"
	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

// Known worker type label values used for per-type metric emission.
const (
	workerTypeTranscodeCpu   = "transcodecpu"
	workerTypeTranscodeGpu   = "transcodegpu"
	workerTypeHealthCheckCpu = "healthcheckcpu"
	workerTypeHealthCheckGpu = "healthcheckgpu"
	workerTypeUnknown        = "unknown"
)

// knownWorkerTypes is the ordered list of all known worker types emitted for
// per-type gauges (worker_count, worker_limit, queue_length). Always emitting
// all four ensures zero-value series appear even when no workers are active.
var knownWorkerTypes = []string{
	workerTypeTranscodeCpu,
	workerTypeTranscodeGpu,
	workerTypeHealthCheckCpu,
	workerTypeHealthCheckGpu,
}

type TdarrNodeMetrics struct {
	// identity / info
	nodeInfo *prometheus.Desc
	// resource stats
	nodeUptime         *prometheus.Desc
	nodeHeapUsedMb     *prometheus.Desc
	nodeHeapTotalMb    *prometheus.Desc
	nodeHostCpuPercent *prometheus.Desc
	nodeHostMemUsedGb  *prometheus.Desc
	nodeHostMemTotalGb *prometheus.Desc
	// node state gauges
	nodePaused          *prometheus.Desc
	nodeMaxGpuWorkers   *prometheus.Desc
	nodeScheduleEnabled *prometheus.Desc
	// per-type node gauges (type label = transcodecpu|transcodegpu|healthcheckcpu|healthcheckgpu)
	nodeWorkerCount *prometheus.Desc
	nodeWorkerLimit *prometheus.Desc
	nodeQueueLength *prometheus.Desc
	// worker identity / info
	nodeWorkerInfo *prometheus.Desc
	// per-worker numeric gauges
	nodeWorkerPercentage         *prometheus.Desc
	nodeWorkerFps                *prometheus.Desc
	nodeWorkerOriginalFileSizeGb *prometheus.Desc
	nodeWorkerOutputFileSizeGb   *prometheus.Desc
	nodeWorkerEstFileSizeGb      *prometheus.Desc
	nodeWorkerJobStartTimestamp  *prometheus.Desc
	nodeWorkerStartTimestamp     *prometheus.Desc
	nodeWorkerStatusTimestamp    *prometheus.Desc
	nodeWorkerEtaSeconds         *prometheus.Desc
	nodeWorkerPid                *prometheus.Desc
}

type TdarrNodeCollector struct {
	config  config.Config
	metrics *TdarrNodeMetrics
}

func NewTdarrNodeMetrics(runConfig config.Config) *TdarrNodeMetrics {
	nodeLabelPair := []string{"node_id", "node_name"}
	nodeTypeLabelPair := []string{"node_id", "node_name", "type"}
	workerLabelPair := []string{"node_id", "node_name", "worker_id"}
	instance := prometheus.Labels{"tdarr_instance": runConfig.InstanceName}

	return &TdarrNodeMetrics{
		nodeInfo: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_info"),
			"Tdarr node identity information",
			[]string{"node_id", "node_name", "gpu_select", "node_pid", "node_priority",
				"allow_gpu_do_cpu", "node_paused"},
			instance,
		),
		nodeUptime: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_uptime_seconds"),
			"Tdarr node uptime in seconds",
			nodeLabelPair,
			instance,
		),
		nodeHeapUsedMb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_heap_used_mb"),
			"Tdarr node heap used in MB",
			nodeLabelPair,
			instance,
		),
		nodeHeapTotalMb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_heap_total_mb"),
			"Tdarr node heap total in MB",
			nodeLabelPair,
			instance,
		),
		nodeHostCpuPercent: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_host_cpu_percent"),
			"Tdarr node cpu percent used",
			nodeLabelPair,
			instance,
		),
		nodeHostMemUsedGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_host_mem_used_gb"),
			"Memory used in GB for host that Tdarr node is running on",
			nodeLabelPair,
			instance,
		),
		nodeHostMemTotalGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_host_mem_total_gb"),
			"Total memory in GB for host that Tdarr node is running on",
			nodeLabelPair,
			instance,
		),
		nodePaused: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_paused"),
			"1 if the Tdarr node is paused, 0 otherwise",
			nodeLabelPair,
			instance,
		),
		nodeMaxGpuWorkers: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_max_gpu_workers"),
			"Maximum number of GPU workers configured for the Tdarr node",
			nodeLabelPair,
			instance,
		),
		nodeScheduleEnabled: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_schedule_enabled"),
			"1 if scheduled operation is enabled on the Tdarr node, 0 otherwise",
			nodeLabelPair,
			instance,
		),
		nodeWorkerCount: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_count"),
			"Number of active workers on the Tdarr node by type",
			nodeTypeLabelPair,
			instance,
		),
		nodeWorkerLimit: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_limit"),
			"Configured worker limit on the Tdarr node by type",
			nodeTypeLabelPair,
			instance,
		),
		nodeQueueLength: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_queue_length"),
			"Current queue length on the Tdarr node by type",
			nodeTypeLabelPair,
			instance,
		),
		nodeWorkerInfo: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_info"),
			"Tdarr node worker identity and categorical state (always 1)",
			[]string{"node_id", "node_name", "worker_id", "worker_type", "flow_worker",
				"worker_status", "worker_file",
				"worker_plugin_id", "worker_plugin_position",
				"worker_connected", "worker_idle"},
			instance,
		),
		nodeWorkerPercentage: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_percentage"),
			"Tdarr node worker transcode/healthcheck progress percentage",
			workerLabelPair,
			instance,
		),
		nodeWorkerFps: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_fps"),
			"Tdarr node worker frames per second",
			workerLabelPair,
			instance,
		),
		nodeWorkerOriginalFileSizeGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_original_file_size_gb"),
			"Tdarr node worker original file size in GB",
			workerLabelPair,
			instance,
		),
		nodeWorkerOutputFileSizeGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_output_file_size_gb"),
			"Tdarr node worker current output file size in GB",
			workerLabelPair,
			instance,
		),
		nodeWorkerEstFileSizeGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_est_file_size_gb"),
			"Tdarr node worker estimated output file size in GB",
			workerLabelPair,
			instance,
		),
		nodeWorkerJobStartTimestamp: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_job_start_timestamp_seconds"),
			"Tdarr node worker job start time as Unix timestamp in seconds",
			workerLabelPair,
			instance,
		),
		nodeWorkerStartTimestamp: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_start_timestamp_seconds"),
			"Tdarr node worker current plugin step start time as Unix timestamp in seconds",
			workerLabelPair,
			instance,
		),
		nodeWorkerStatusTimestamp: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_status_timestamp_seconds"),
			"Tdarr node worker last status update time as Unix timestamp in seconds",
			workerLabelPair,
			instance,
		),
		nodeWorkerEtaSeconds: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_eta_seconds"),
			"Tdarr node worker estimated time remaining in seconds",
			workerLabelPair,
			instance,
		),
		nodeWorkerPid: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_pid"),
			"Tdarr node worker process ID",
			workerLabelPair,
			instance,
		),
	}
}

func NewTdarrNodeCollector(runConfig config.Config) *TdarrNodeCollector {
	return &TdarrNodeCollector{
		config:  runConfig,
		metrics: NewTdarrNodeMetrics(runConfig),
	}
}

func (n *TdarrNodeCollector) GetNodeData() (map[string]TdarrNode, error) {
	httpClient, err := client.NewRequestClient(n.config.UrlParsed, n.config.VerifySsl, n.config.ApiKey)
	if err != nil {
		log.Error().
			Err(err).Msg("Failed to create http request client for Tdarr, ensure proper URL is provided")
		return nil, err
	}
	// get node data
	nodeData := map[string]TdarrNode{}
	nodeHttpErr := httpClient.DoRequest(n.config.TdarrNodePath, &nodeData)
	if nodeHttpErr != nil {
		log.Error().Err(nodeHttpErr).Msg("Failed to get node data for Tdarr exporter")
		return nil, nodeHttpErr
	}
	log.Debug().Interface("response", nodeData).Msg("Node Api Response")
	return nodeData, nil
}

// emitPerType emits a gauge metric for all four known worker types using values
// from the provided TdarrNodeJobs struct. This ensures zero values are always
// emitted even when no workers of a given type are active.
func emitPerType(ch chan<- prometheus.Metric, desc *prometheus.Desc, nodeId, nodeName string, jobs TdarrNodeJobs) {
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(jobs.TranscodeCpu), nodeId, nodeName, workerTypeTranscodeCpu)
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(jobs.TranscodeGpu), nodeId, nodeName, workerTypeTranscodeGpu)
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(jobs.HealthCheckCpu), nodeId, nodeName, workerTypeHealthCheckCpu)
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(jobs.HealthCheckGpu), nodeId, nodeName, workerTypeHealthCheckGpu)
}

// countWorkersByType counts active workers in the provided workers map grouped
// by their WorkerType field. Unknown types are bucketed under workerTypeUnknown
// and a warning is logged for each distinct unknown type encountered.
func countWorkersByType(workers map[string]TdarrNodeWorkers) map[string]int {
	counts := map[string]int{
		workerTypeTranscodeCpu:   0,
		workerTypeTranscodeGpu:   0,
		workerTypeHealthCheckCpu: 0,
		workerTypeHealthCheckGpu: 0,
	}
	for _, w := range workers {
		if _, known := counts[w.WorkerType]; known {
			counts[w.WorkerType]++
		} else {
			log.Warn().Str("workerType", w.WorkerType).Msg("Unknown worker type encountered; bucketing under 'unknown'")
			counts[workerTypeUnknown]++
		}
	}
	return counts
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
