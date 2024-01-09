package collector

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeylab/tdarr-exporter/internal/client"
	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

var (
	METRIC_PREFIX = "tdarr"
)

type TdarrCollector struct {
	config                config.Config
	payload               TdarrMetricRequest
	totalFilesMetric      *prometheus.Desc
	totalTranscodeCount   *prometheus.Desc
	totalHealthCheckCount *prometheus.Desc
	sizeDiff              *prometheus.Desc
	tdarrScore            *prometheus.Desc
	healthCheckScore      *prometheus.Desc
	avgNumStreams         *prometheus.Desc
	streamStatsDuration   *prometheus.Desc
	streamStatsBitRate    *prometheus.Desc
	streamStatsNumFrames  *prometheus.Desc
	pieNumFiles           *prometheus.Desc
	pieNumTranscodes      *prometheus.Desc
	pieNumHealthChecks    *prometheus.Desc
	pieSizeDiff           *prometheus.Desc
	pieTranscodes         *prometheus.Desc
	pieHealthChecks       *prometheus.Desc
	pieVideoCodecs        *prometheus.Desc
	pieVideoContainers    *prometheus.Desc
	pieVideoResolutions   *prometheus.Desc
	pieAudioCodecs        *prometheus.Desc
	pieAudioContainers    *prometheus.Desc
	// node data
	nodeMetrics *TdarrNodeMetrics
	// nodeWorkerLimit *prometheus.Desc
	// nodePaused      *prometheus.Desc
	// nodePriority    *prometheus.Desc
	// nodeUptime      *prometheus.Desc
	// nodeHeapUsedMb  *prometheus.Desc
	// nodeHeapTotalMb *prometheus.Desc
	// nodeCpuPercent  *prometheus.Desc
	// nodeMemUsedGb   *prometheus.Desc
	// nodeMemTotalGb  *prometheus.Desc
	// nodeQueueLength *prometheus.Desc
	errorMetric *prometheus.Desc // Error Description for use with InvalidMetric
}

type TdarrNodeMetrics struct {
	// node data
	nodeWorkerLimit                *prometheus.Desc
	nodePaused                     *prometheus.Desc
	nodePriority                   *prometheus.Desc
	nodeUptime                     *prometheus.Desc
	nodeHeapUsedMb                 *prometheus.Desc
	nodeHeapTotalMb                *prometheus.Desc
	nodeCpuPercent                 *prometheus.Desc
	nodeMemUsedGb                  *prometheus.Desc
	nodeMemTotalGb                 *prometheus.Desc
	nodeQueueLength                *prometheus.Desc
	workerFps                      *prometheus.Desc
	workerIdle                     *prometheus.Desc
	workerOriginalFileSizeGb       *prometheus.Desc
	workerPercentage               *prometheus.Desc
	workerEta                      *prometheus.Desc
	workerStartTime                *prometheus.Desc
	workerProcessStartTime         *prometheus.Desc
	workerProcessPluginPosition    *prometheus.Desc
	workerProcessPluginPositionMax *prometheus.Desc
	workerProcessOutputSizeGb      *prometheus.Desc
	workerProcessEstSizeGb         *prometheus.Desc
}

func NewTdarrNodeMetrics(runConfig config.Config) *TdarrNodeMetrics {
	return &TdarrNodeMetrics{
		nodeWorkerLimit: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_limit"),
			"Tdarr node health check cpu limit",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select", "worker_type"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodePaused: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_paused"),
			"Tdarr node paused: 1 = paused, 0 = not paused",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodePriority: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_priority"),
			"Tdarr node priority",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeUptime: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_uptime_seconds"),
			"Tdarr node uptime in seconds",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeHeapUsedMb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_heap_used_mb"),
			"Tdarr node heap used in MB",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeHeapTotalMb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_heap_total_mb"),
			"Tdarr node heap total in MB",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeCpuPercent: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_cpu_percent"),
			"Tdarr node cpu percent used",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeMemUsedGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_mem_used_gb"),
			"Tdarr node memory used in GB",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeMemTotalGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_mem_total_gb"),
			"Tdarr node memory total in GB",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeQueueLength: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_queue_length"),
			"Tdarr node queue length for a given worker type",
			[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select", "worker_type"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerFps: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_fps"),
			"Tdarr worker fps for job running on node",
			[]string{"node_id", "node_name", "worker_id", "worker_type", "worker_status", "worker_file", "worker_status"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerIdle: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_job_idle"),
			"Tdarr worker idle status on node: 1 = idle, 0 = not idle",
			[]string{"node_id", "node_name", "worker_id", "worker_type", "worker_status", "worker_file", "worker_status"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerOriginalFileSizeGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_job_original_file_size_gb"),
			"The original file size, in GB, for the worker job running on node",
			[]string{"node_id", "node_name", "worker_id", "worker_type", "worker_status", "worker_file", "worker_status"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerPercentage: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_percentage"),
			"The percentage completed for the worker job running on node",
			[]string{"worker_id", "worker_type", "worker_status", "worker_file", "worker_status", "job_type", "job_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerEta: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_eta"),
			"The estimated time remaining (unix timestamp) for the worker job running on node",
			[]string{"worker_id", "worker_type", "worker_status", "worker_file", "worker_status", "job_type", "job_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerStartTime: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_start_time"),
			"The start time (unix timestamp) for the worker job running on node",
			[]string{"worker_id", "worker_type", "worker_status", "worker_file", "worker_status", "job_type", "job_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerProcessStartTime: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_process_start_time"),
			"The start time (unix timestamp) for the worker job process running on node",
			[]string{"worker_id", "worker_type", "worker_status", "worker_file", "worker_status", "job_type", "job_id", "process_pid"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerProcessPluginPosition: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_process_plugin_position"),
			"The position number for the worker job process running on node",
			[]string{"worker_id", "worker_type", "worker_status", "worker_file", "worker_status", "job_type", "job_id", "process_pid", "plugin_source", "plugin_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerProcessPluginPositionMax: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_process_plugin_position_max"),
			"The position max for the worker job process running on node",
			[]string{"worker_id", "worker_type", "worker_status", "worker_file", "worker_status", "job_type", "job_id", "process_pid", "plugin_source", "plugin_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerProcessOutputSizeGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_process_output_size_gb"),
			"The output size in GB for the worker job process running on node",
			[]string{"worker_id", "worker_type", "worker_status", "worker_file", "worker_status", "job_type", "job_id", "process_pid", "plugin_source", "plugin_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		workerProcessEstSizeGb: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_process_est_size_gb"),
			"The estimated size in GB for the worker job process running on node",
			[]string{"worker_id", "worker_type", "worker_status", "worker_file", "worker_status", "job_type", "job_id", "process_pid", "plugin_source", "plugin_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
	}
}

func NewTdarrCollector(runConfig config.Config) *TdarrCollector {
	return &TdarrCollector{
		config:  runConfig,
		payload: getRequestPayload(),
		totalFilesMetric: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "files_total"),
			"Tdarr total file count - includes files in ignore lists within each library",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		totalTranscodeCount: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "transcodes_total"),
			"Tdarr total transcode count for all libraries",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		totalHealthCheckCount: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "health_checks_total"),
			"Tdarr total health check count for all libraries",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		sizeDiff: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "size_diff_gb"),
			"Tdarr size difference (+/-) in GB",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		tdarrScore: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "score_pct"),
			"Tdarr score percentage - how much of your library is being handled by tdarr",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		healthCheckScore: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "health_check_score_pct"),
			"Tdarr health check score percentage - how much of your library is has been health checked by tdarr",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		avgNumStreams: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "avg_num_streams"),
			"Tdarr average number of streams in video",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		streamStatsDuration: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "stream_stats_duration"),
			"Tdarr stream stats duration",
			[]string{"stat_type"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		streamStatsBitRate: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "stream_stats_bit_rate"),
			"Tdarr stream stats bit rate",
			[]string{"stat_type"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		streamStatsNumFrames: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "stream_stats_num_frames"),
			"Tdarr stream stats number of frames",
			[]string{"stat_type"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieNumFiles: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_files_total"),
			"Tdarr total files in library",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieNumTranscodes: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_transcodes_total"),
			"Tdarr total transcodes for library by status",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieNumHealthChecks: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_health_checks_total"),
			"Tdarr total health checks for library by status",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieSizeDiff: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_size_diff_gb"),
			"Tdarr size difference (+/-) in GB for library",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieTranscodes: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_transcodes"),
			"Tdarr transcodes for library by status",
			[]string{"library_name", "library_id", "status"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieHealthChecks: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_health_checks"),
			"Tdarr health checks for library by status",
			[]string{"library_name", "library_id", "status"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieVideoCodecs: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_video_codecs"),
			"Tdarr video codecs for library by type",
			[]string{"library_name", "library_id", "codec"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieVideoContainers: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_video_containers"),
			"Tdarr video containers for library by type",
			[]string{"library_name", "library_id", "container_type"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieVideoResolutions: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_video_resolutions"),
			"Tdarr video resolutions for library by type",
			[]string{"library_name", "library_id", "resolution"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieAudioCodecs: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_audio_codecs"),
			"Tdarr audio codecs for library by type",
			[]string{"library_name", "library_id", "codec"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		pieAudioContainers: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_audio_containers"),
			"Tdarr video containers for library by type",
			[]string{"library_name", "library_id", "container_type"},
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
		nodeMetrics: NewTdarrNodeMetrics(runConfig),
		// nodeWorkerLimit: prometheus.NewDesc(
		// 	prometheus.BuildFQName(METRIC_PREFIX, "", "node_worker_limit"),
		// 	"Tdarr node health check cpu limit",
		// 	[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select", "worker_type"},
		// 	prometheus.Labels{"tdarr_instance": runConfig.Url},
		// ),
		// nodePaused: prometheus.NewDesc(
		// 	prometheus.BuildFQName(METRIC_PREFIX, "", "node_paused"),
		// 	"Tdarr node paused, 1 = paused, 0 = no",
		// 	[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
		// 	prometheus.Labels{"tdarr_instance": runConfig.Url},
		// ),
		// nodePriority: prometheus.NewDesc(
		// 	prometheus.BuildFQName(METRIC_PREFIX, "", "node_priority"),
		// 	"Tdarr node priority",
		// 	[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
		// 	prometheus.Labels{"tdarr_instance": runConfig.Url},
		// ),
		// nodeUptime: prometheus.NewDesc(
		// 	prometheus.BuildFQName(METRIC_PREFIX, "", "node_uptime_seconds"),
		// 	"Tdarr node uptime in seconds",
		// 	[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
		// 	prometheus.Labels{"tdarr_instance": runConfig.Url},
		// ),
		// nodeHeapUsedMb: prometheus.NewDesc(
		// 	prometheus.BuildFQName(METRIC_PREFIX, "", "node_heap_used_mb"),
		// 	"Tdarr node heap used in MB",
		// 	[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
		// 	prometheus.Labels{"tdarr_instance": runConfig.Url},
		// ),
		// nodeHeapTotalMb: prometheus.NewDesc(
		// 	prometheus.BuildFQName(METRIC_PREFIX, "", "node_heap_total_mb"),
		// 	"Tdarr node heap total in MB",
		// 	[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
		// 	prometheus.Labels{"tdarr_instance": runConfig.Url},
		// ),
		// nodeCpuPercent: prometheus.NewDesc(
		// 	prometheus.BuildFQName(METRIC_PREFIX, "", "node_cpu_percent"),
		// 	"Tdarr node cpu percent used",
		// 	[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
		// 	prometheus.Labels{"tdarr_instance": runConfig.Url},
		// ),
		// nodeMemUsedGb: prometheus.NewDesc(
		// 	prometheus.BuildFQName(METRIC_PREFIX, "", "node_mem_used_gb"),
		// 	"Tdarr node memory used in GB",
		// 	[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
		// 	prometheus.Labels{"tdarr_instance": runConfig.Url},
		// ),
		// nodeMemTotalGb: prometheus.NewDesc(
		// 	prometheus.BuildFQName(METRIC_PREFIX, "", "node_mem_total_gb"),
		// 	"Tdarr node memory total in GB",
		// 	[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select"},
		// 	prometheus.Labels{"tdarr_instance": runConfig.Url},
		// ),
		// nodeQueueLength: prometheus.NewDesc(
		// 	prometheus.BuildFQName(METRIC_PREFIX, "", "node_queue_length"),
		// 	"Tdarr node queue length for a given worker type",
		// 	[]string{"node_id", "node_name", "node_address", "server_address", "server_port", "gpu_select", "worker_type"},
		// 	prometheus.Labels{"tdarr_instance": runConfig.Url},
		// ),
		errorMetric: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "collector_error"),
			"Error while collecting metrics",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.Url},
		),
	}
}

func (c *TdarrCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.totalFilesMetric
	ch <- c.totalTranscodeCount
	ch <- c.totalHealthCheckCount
	ch <- c.sizeDiff
	ch <- c.tdarrScore
	ch <- c.healthCheckScore
	ch <- c.pieNumFiles
	ch <- c.pieNumTranscodes
	ch <- c.pieNumHealthChecks
	ch <- c.pieSizeDiff
	ch <- c.pieTranscodes
	ch <- c.pieHealthChecks
	ch <- c.avgNumStreams
	ch <- c.streamStatsDuration
	ch <- c.streamStatsBitRate
	ch <- c.pieVideoCodecs
	ch <- c.pieVideoContainers
	ch <- c.pieVideoResolutions
	ch <- c.pieAudioCodecs
	ch <- c.pieAudioContainers
	ch <- c.nodeMetrics.nodeWorkerLimit
	ch <- c.nodeMetrics.nodePaused
	ch <- c.nodeMetrics.nodePriority
	ch <- c.nodeMetrics.nodeUptime
	ch <- c.nodeMetrics.nodeHeapUsedMb
	ch <- c.nodeMetrics.nodeHeapTotalMb
	ch <- c.nodeMetrics.nodeCpuPercent
	ch <- c.nodeMetrics.nodeMemUsedGb
	ch <- c.nodeMetrics.nodeMemTotalGb
	ch <- c.nodeMetrics.nodeQueueLength
	// ch <- c.nodeWorkerLimit
	// ch <- c.nodePaused
	// ch <- c.nodePriority
	// ch <- c.nodeUptime
	// ch <- c.nodeHeapUsedMb
	// ch <- c.nodeHeapTotalMb
	// ch <- c.nodeCpuPercent
	// ch <- c.nodeMemUsedGb
	// ch <- c.nodeMemTotalGb
	// ch <- c.nodeQueueLength
}

func (c *TdarrCollector) getMetricsResponse() (*TdarrMetric, error) {
	httpClient, err := client.NewRequestClient(c.config.Url, c.config.VerifySsl)
	if err != nil {
		log.Error().
			Err(err).Msg("Failed to create http request client for Tdarr, ensure proper URL is provided")
		return nil, err
	}
	log.Debug().Interface("payload", c.payload).Msg("Requesting statistics data from Tdarr")
	// Marshal it into JSON prior to requesting
	payload, err := json.Marshal(c.payload)
	if err != nil {
		log.Error().Err(err).Interface("payload", c.payload).
			Msg("Failed to marshal payload for statistics request")
		return nil, err
	}
	// get the post request payload to use
	metric := &TdarrMetric{}
	// make request
	httpErr := httpClient.DoPostRequest(c.config.TdarrMetricsPath, metric, payload)
	if httpErr != nil {
		log.Error().Err(httpErr).Msg("Failed to get data for Tdarr exporter")
		return nil, httpErr
	}
	log.Debug().Interface("response", metric).Msg("Metrics Api Response")
	return metric, nil
}

func (c *TdarrCollector) getNodeData() (map[string]TdarrNode, error) {
	httpClient, err := client.NewRequestClient(c.config.Url, c.config.VerifySsl)
	if err != nil {
		log.Error().
			Err(err).Msg("Failed to create http request client for Tdarr, ensure proper URL is provided")
		return nil, err
	}
	// get node data
	nodeData := map[string]TdarrNode{}
	nodeHttpErr := httpClient.DoRequest(c.config.TdarrNodePath, &nodeData)
	if nodeHttpErr != nil {
		log.Error().Err(nodeHttpErr).Msg("Failed to get node data for Tdarr exporter")
		return nil, nodeHttpErr
	}
	log.Info().Interface("response", nodeData).Msg("Node Api Response")
	return nodeData, nil
}

func (c *TdarrCollector) Collect(ch chan<- prometheus.Metric) {
	// get server metrics
	metric, err := c.getMetricsResponse()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
		return
	}

	// get all nodes and their metrics
	nodeData, err := c.getNodeData()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
		return
	}
	fmt.Println(nodeData)
	// get metrics data
	var (
		pieData         []TdarrPie
		score           float64
		healthScore     float64
		floatConvertErr error
	)

	score, floatConvertErr = strconv.ParseFloat(metric.TdarrScore, 64)
	if floatConvertErr != nil {
		log.Error().Str("tdarrScoreStr", metric.TdarrScore).Err(floatConvertErr).Msg("Failed to convert a tdarr score to float")
		ch <- prometheus.NewInvalidMetric(c.errorMetric, floatConvertErr)
		return
	}
	healthScore, floatConvertErr = strconv.ParseFloat(metric.HealthCheckScore, 64)
	if floatConvertErr != nil {
		log.Error().Str("healthCodeStr", metric.HealthCheckScore).Err(floatConvertErr).Msg("Failed to convert a health score to float")
		ch <- prometheus.NewInvalidMetric(c.errorMetric, floatConvertErr)
		return
	}

	for _, pie := range metric.Pies {
		log.Debug().Interface("pie", pie).Msg("Pie data to be parsed")
		var (
			transcodePie        []TdarrPieSlice
			healthPie           []TdarrPieSlice
			videoCodecsPie      []TdarrPieSlice
			videoContainersPie  []TdarrPieSlice
			videoResolutionsPie []TdarrPieSlice
			audioCodecsPie      []TdarrPieSlice
			audioContainersPie  []TdarrPieSlice
			pieMetricsErr       error
		)
		if transcodePie, pieMetricsErr = getPieMetricsFields(pie[6].([]interface{})); pieMetricsErr != nil {
			log.Error().Interface("rawData", pie[6].([]interface{})).Msg("Failed to get transcode pie metrics")
			ch <- prometheus.NewInvalidMetric(c.errorMetric, pieMetricsErr)
		}
		if healthPie, pieMetricsErr = getPieMetricsFields(pie[7].([]interface{})); pieMetricsErr != nil {
			log.Error().Interface("rawData", pie[7].([]interface{})).Msg("Failed to get health pie metrics")
			ch <- prometheus.NewInvalidMetric(c.errorMetric, pieMetricsErr)
		}
		if videoCodecsPie, pieMetricsErr = getPieMetricsFields(pie[8].([]interface{})); pieMetricsErr != nil {
			log.Error().Interface("rawData", pie[8].([]interface{})).Msg("Failed to get video codecs pie metrics")
			ch <- prometheus.NewInvalidMetric(c.errorMetric, pieMetricsErr)
		}
		if videoContainersPie, pieMetricsErr = getPieMetricsFields(pie[9].([]interface{})); pieMetricsErr != nil {
			log.Error().Interface("rawData", pie[9].([]interface{})).Msg("Failed to get video containers pie metrics")
			ch <- prometheus.NewInvalidMetric(c.errorMetric, pieMetricsErr)
		}
		if videoResolutionsPie, pieMetricsErr = getPieMetricsFields(pie[10].([]interface{})); pieMetricsErr != nil {
			log.Error().Interface("rawData", pie[10].([]interface{})).Msg("Failed to get video resolutions pie metrics")
			ch <- prometheus.NewInvalidMetric(c.errorMetric, pieMetricsErr)
		}
		if audioCodecsPie, pieMetricsErr = getPieMetricsFields(pie[11].([]interface{})); pieMetricsErr != nil {
			log.Error().Interface("rawData", pie[11].([]interface{})).Msg("Failed to get audio codecs pie metrics")
			ch <- prometheus.NewInvalidMetric(c.errorMetric, pieMetricsErr)
		}
		if audioContainersPie, pieMetricsErr = getPieMetricsFields(pie[12].([]interface{})); pieMetricsErr != nil {
			log.Error().Interface("rawData", pie[12].([]interface{})).Msg("Failed to get audio containers pie metrics")
			ch <- prometheus.NewInvalidMetric(c.errorMetric, pieMetricsErr)
		}
		pieData = append(pieData, TdarrPie{
			LibraryName:              pie[0].(string),
			LibraryId:                pie[1].(string),
			NumFiles:                 pie[2].(float64),
			NumTranscodes:            pie[3].(float64),
			SpaceSavedGB:             pie[4].(float64),
			NumHealthChecks:          pie[5].(float64),
			TdarrTranscodePie:        transcodePie,
			TdarrHealthPie:           healthPie,
			TdarrVideoCodecsPie:      videoCodecsPie,
			TdarrVideoContainersPie:  videoContainersPie,
			TdarrVideoResolutionsPie: videoResolutionsPie,
			TdarrAudioCodecsPie:      audioCodecsPie,
			TdarrAudioContainersPie:  audioContainersPie,
		})
	}

	// add metrics to collector
	ch <- prometheus.MustNewConstMetric(c.totalFilesMetric, prometheus.GaugeValue, float64(metric.TotalFileCount))
	ch <- prometheus.MustNewConstMetric(c.totalTranscodeCount, prometheus.GaugeValue, float64(metric.TotalTranscodeCount))
	ch <- prometheus.MustNewConstMetric(c.totalHealthCheckCount, prometheus.GaugeValue, float64(metric.TotalHealthCheckCount))
	ch <- prometheus.MustNewConstMetric(c.sizeDiff, prometheus.GaugeValue, metric.SizeDiff)
	ch <- prometheus.MustNewConstMetric(c.tdarrScore, prometheus.GaugeValue, score)
	ch <- prometheus.MustNewConstMetric(c.healthCheckScore, prometheus.GaugeValue, healthScore)
	ch <- prometheus.MustNewConstMetric(c.avgNumStreams, prometheus.GaugeValue, metric.AvgNumStreams)
	ch <- prometheus.MustNewConstMetric(c.streamStatsDuration, prometheus.GaugeValue, float64(metric.StreamStats.Duration.Average), "average")
	ch <- prometheus.MustNewConstMetric(c.streamStatsDuration, prometheus.GaugeValue, float64(metric.StreamStats.Duration.Highest), "highest")
	ch <- prometheus.MustNewConstMetric(c.streamStatsDuration, prometheus.GaugeValue, float64(metric.StreamStats.Duration.Total), "total")
	ch <- prometheus.MustNewConstMetric(c.streamStatsBitRate, prometheus.GaugeValue, float64(metric.StreamStats.BitRate.Average), "average")
	ch <- prometheus.MustNewConstMetric(c.streamStatsBitRate, prometheus.GaugeValue, float64(metric.StreamStats.BitRate.Highest), "highest")
	ch <- prometheus.MustNewConstMetric(c.streamStatsBitRate, prometheus.GaugeValue, float64(metric.StreamStats.BitRate.Total), "total")
	ch <- prometheus.MustNewConstMetric(c.streamStatsNumFrames, prometheus.GaugeValue, float64(metric.StreamStats.NumFrames.Average), "average")
	ch <- prometheus.MustNewConstMetric(c.streamStatsNumFrames, prometheus.GaugeValue, float64(metric.StreamStats.NumFrames.Highest), "highest")
	ch <- prometheus.MustNewConstMetric(c.streamStatsNumFrames, prometheus.GaugeValue, float64(metric.StreamStats.NumFrames.Total), "total")
	for _, pie := range pieData {
		libraryId := pie.LibraryId
		// if we don't change, it errors with duplicate label value in single metric with `library_name` label
		if strings.ToLower(libraryId) == "all" {
			libraryId = "all_libraries"
		}
		ch <- prometheus.MustNewConstMetric(c.pieNumFiles, prometheus.GaugeValue, pie.NumFiles, pie.LibraryName, libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieNumTranscodes, prometheus.GaugeValue, pie.NumTranscodes, pie.LibraryName, libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieNumHealthChecks, prometheus.GaugeValue, pie.NumHealthChecks, pie.LibraryName, libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieSizeDiff, prometheus.GaugeValue, pie.SpaceSavedGB, pie.LibraryName, libraryId)
		for _, pieSlice := range pie.TdarrTranscodePie {
			var labelName string
			statusName := strings.ToLower(pieSlice.Name)
			if strings.HasPrefix(statusName, "transcode") {
				// remove the Transcode prefix
				labelName = cleanUpTranscodeStatus(statusName, true)
			} else {
				labelName = cleanUpTranscodeStatus(statusName, false)
			}
			ch <- prometheus.MustNewConstMetric(c.pieTranscodes, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.LibraryName, libraryId, labelName)
		}
		for _, pieSlice := range pie.TdarrHealthPie {
			ch <- prometheus.MustNewConstMetric(c.pieHealthChecks, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.LibraryName, libraryId, strings.ToLower(pieSlice.Name))
		}
		for _, pieSlice := range pie.TdarrVideoCodecsPie {
			ch <- prometheus.MustNewConstMetric(c.pieVideoCodecs, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.LibraryName, libraryId, strings.ToLower(pieSlice.Name))
		}
		for _, pieSlice := range pie.TdarrVideoContainersPie {
			ch <- prometheus.MustNewConstMetric(c.pieVideoContainers, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.LibraryName, libraryId, strings.ToLower(pieSlice.Name))
		}
		for _, pieSlice := range pie.TdarrVideoResolutionsPie {
			ch <- prometheus.MustNewConstMetric(c.pieVideoResolutions, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.LibraryName, libraryId, strings.ToLower(pieSlice.Name))
		}
		for _, pieSlice := range pie.TdarrAudioCodecsPie {
			ch <- prometheus.MustNewConstMetric(c.pieAudioCodecs, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.LibraryName, libraryId, strings.ToLower(pieSlice.Name))
		}
		for _, pieSlice := range pie.TdarrAudioContainersPie {
			ch <- prometheus.MustNewConstMetric(c.pieAudioContainers, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.LibraryName, libraryId, strings.ToLower(pieSlice.Name))
		}
	}
	// node data parsing
	for _, node := range nodeData {
		ch <- prometheus.MustNewConstMetric(c.nodeMetrics.nodeWorkerLimit, prometheus.GaugeValue, float64(node.WorkerLimits.HealthCheckCpu),
			node.Id, node.Name, node.RemoteAddress, node.Config.ServerIp, node.Config.ServerPort, node.GpuSelect, "healthcheckcpu")
	}

}

func getPieMetricsFields(data []interface{}) (pieSlice []TdarrPieSlice, err error) {
	for _, metricDetail := range data {
		currSlice := TdarrPieSlice{}
		err = loadKeyValue(metricDetail, &currSlice)
		if err != nil {
			log.Error().Err(err).Interface("metricDetail", metricDetail).Msg("Failed to unmarshal pie slice data")
			return
		}
		pieSlice = append(pieSlice, currSlice)
	}
	return
}

func cleanUpTranscodeStatus(status string, transcodeFlag bool) (newStatus string) {
	if transcodeFlag {
		newStatus = strings.Replace(status, "transcode", "", 1)
	} else {
		newStatus = status
	}
	newStatus = strings.TrimSpace(newStatus)
	return
}

func loadKeyValue(data interface{}, target *TdarrPieSlice) (err error) {
	bodyBytes, _ := json.Marshal(data)
	err = json.Unmarshal(bodyBytes, &target)
	return
}

func getRequestPayload() TdarrMetricRequest {
	return TdarrMetricRequest{
		Data: TdarrDataRequest{
			Collection: "StatisticsJSONDB",
			Mode:       "getById",
			DocId:      "statistics",
			Obj:        map[string]interface{}{},
		},
	}
}
