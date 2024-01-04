var (
	METRIC_PREFIX = "tdarr"
)

type TdarrNodeCollector struct {
	config                config.Config
	payload               TdarrMetricRequest
	totalFilesMetric      *prometheus.Desc
	totalTranscodeCount   *prometheus.Desc
	totalHealthCheckCount *prometheus.Desc
}

func NewTdarrNodeCollector(runConfig config.Config) *TdarrNodeCollector {
	return &TdarrNodeCollector{
		config: runConfig,
		nodeWorkerLimit: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_limit"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select", "worker_type"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodePaused: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_paused"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodePriority: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeUptime: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeHeapUsedMb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeHeapTotalMb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeCpuPercent: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeMemUsedGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeMemTotalGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeQueueLength: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_queue_length"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select", "worker_type"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
	}
}

func (c *TdarrNodeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.nodeWorkerLimit
	ch <- c.nodePaused
	ch <- c.nodeResourceStats
}

func (c *TdarrNodeCollector) Collect(ch chan<- prometheus.Metric) {
	httpClient, err := client.NewRequestClient(c.config.Url, c.config.VerifySsl)
	if err != nil {
		log.Error().
			Err(err).Msg("Failed to create http request client for Tdarr, ensure proper URL is provided")
		ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
	}
	// get node data
	nodeData := map[string]TdarrNode{}
	nodeHttpErr := httpClient.DoRequest(c.config.TdarrNodePath, nodeData)
	if nodeHttpErr != nil {
		log.Error().Err(nodeHttpErr).Msg("Failed to get node data for Tdarr exporter")
		ch <- prometheus.NewInvalidMetric(c.errorMetric, nodeHttpErr)
		return
	}
	log.Debug().Interface("response", metric).Msg("Node Api Response")

}