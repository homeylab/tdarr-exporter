package collector

import (
	"github.com/homeylab/tdarr-exporter/internal/client"
	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

type TdarrNodeMetrics struct {
	// node data
	nodeInfo           *prometheus.Desc
	nodeWorkerInfo     *prometheus.Desc
	nodeWorkerFlowInfo *prometheus.Desc
	nodeUptime         *prometheus.Desc
	nodeHeapUsedMb     *prometheus.Desc
	nodeHeapTotalMb    *prometheus.Desc
	nodeHostCpuPercent *prometheus.Desc
	nodeHostMemUsedGb  *prometheus.Desc
	nodeHostMemTotalGb *prometheus.Desc
}

type TdarrNodeCollector struct {
	config  config.Config
	metrics *TdarrNodeMetrics
}

func NewTdarrNodeMetrics(runConfig config.Config) *TdarrNodeMetrics {
	return &TdarrNodeMetrics{
		nodeInfo: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_info"),
			"Tdarr node info",
			[]string{"node_id", "node_name", "gpu_select", "node_priority", "node_pid", "node_paused",
				"node_gpu_health_check_limit", "node_cpu_health_check_limit",
				"node_gpu_transcode_limit", "node_cpu_transcode_limit",
				"node_health_check_gpu_queue", "node_health_check_cpu_queue",
				"node_transcode_gpu_queue", "node_transcode_cpu_queue"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeUptime: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_uptime_seconds"),
			"Tdarr node uptime in seconds",
			[]string{"node_id", "node_name"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeHeapUsedMb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_heap_used_mb"),
			"Tdarr node heap used in MB",
			[]string{"node_id", "node_name"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeHeapTotalMb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_heap_total_mb"),
			"Tdarr node heap total in MB",
			[]string{"node_id", "node_name"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeHostCpuPercent: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_host_cpu_percent"),
			"Tdarr node cpu percent used",
			[]string{"node_id", "node_name"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeHostMemUsedGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_host_mem_used_gb"),
			"Memory used in GB for host that Tdarr node is running on",
			[]string{"node_id", "node_name"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeHostMemTotalGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_host_mem_total_gb"),
			"Total memory in GB for host that Tdarr node is running on",
			[]string{"node_id", "node_name"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeWorkerInfo: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_info"),
			"Tdarr node worker info",
			[]string{"node_id", "node_name", "worker_id", "worker_type",
				"worker_status", "worker_status_ts", "worker_idle",
				"worker_file", "worker_original_file_size_gb",
				"worker_fps", "worker_eta",
				"worker_percentage", "worker_connected", "worker_pid",
				"worker_job_start_ts", "worker_job_process_start_ts", // when the operation started vs when plugin step within the operation started
				"worker_plugin_id", "worker_plugin_position",
				"worker_output_size_gb", "worker_est_size_gb"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeWorkerFlowInfo: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_flow_info"),
			"Tdarr node worker flow process info",
			[]string{"node_id", "node_name", "worker_id", "worker_type",
				"worker_status", "worker_status_ts", "worker_idle",
				"worker_file", "worker_original_file_size_gb",
				"worker_fps", "worker_eta",
				"worker_percentage", "worker_connected", "worker_pid",
				"worker_job_start_ts", "worker_job_process_start_ts",
				"worker_output_size_gb", "worker_est_size_gb"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
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
	httpClient, err := client.NewRequestClient(n.config.Url, n.config.VerifySsl)
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
