package collector

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"

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
	statsCache            *TdarrLibStatsCache
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
	nodeCollector         *TdarrNodeCollector // node data
	errorMetric           *prometheus.Desc    // Error Description for use with InvalidMetric
}

// Cache to store library stats and reduce excessive API calls
// Mutex added to reduce chance of running into errors (accessing same var) from misconfiguration or manual testing
// i.e getting scraped twice by two different prometheus instances
type TdarrLibStatsCache struct {
	mu           sync.RWMutex
	totalFiles   int
	libraryStats []*TdarrPieStats
}

func NewTdarrLibStatsCache() *TdarrLibStatsCache {
	return &TdarrLibStatsCache{
		totalFiles:   0,
		libraryStats: nil,
	}
}

func (c *TdarrLibStatsCache) GetTotalFiles() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalFiles
}

func (c *TdarrLibStatsCache) SetTotalFiles(totalNum int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.totalFiles = totalNum
}

func (c *TdarrLibStatsCache) GetLibStats() []*TdarrPieStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.libraryStats
}

func (c *TdarrLibStatsCache) SetLibStats(stats []*TdarrPieStats) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.libraryStats = stats
}

// collector
func NewTdarrCollector(runConfig config.Config) *TdarrCollector {
	return &TdarrCollector{
		config:     runConfig,
		statsCache: NewTdarrLibStatsCache(),
		totalFilesMetric: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "files_total"),
			"Tdarr total file count - includes files in ignore lists within each library",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		totalTranscodeCount: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "transcodes_total"),
			"Tdarr total transcode count for all libraries",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		totalHealthCheckCount: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "health_checks_total"),
			"Tdarr total health check count for all libraries",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		sizeDiff: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "size_diff_gb"),
			"Tdarr size difference (+/-) in GB",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		tdarrScore: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "score_pct"),
			"Tdarr score percentage - how much of your libraries has been handled by tdarr",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		healthCheckScore: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "health_check_score_pct"),
			"Tdarr health check score percentage - how much of your libraries has been health checked by tdarr",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		avgNumStreams: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "avg_num_streams"),
			"Tdarr average number of streams in video",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		streamStatsDuration: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "stream_stats_duration"),
			"Tdarr stream stats duration",
			[]string{"stat_type"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		streamStatsBitRate: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "stream_stats_bit_rate"),
			"Tdarr stream stats bit rate",
			[]string{"stat_type"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		streamStatsNumFrames: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "stream_stats_num_frames"),
			"Tdarr stream stats number of frames",
			[]string{"stat_type"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieNumFiles: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_files_total"),
			"Tdarr total files in library",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieNumTranscodes: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_transcodes_total"),
			"Tdarr total transcodes for library by status",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieNumHealthChecks: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_health_checks_total"),
			"Tdarr total health checks for library by status",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieSizeDiff: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_size_diff_gb"),
			"Tdarr size difference (+/-) in GB for library",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieTranscodes: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_transcodes"),
			"Tdarr transcodes for library by status",
			[]string{"library_name", "library_id", "status"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieHealthChecks: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_health_checks"),
			"Tdarr health checks for library by status",
			[]string{"library_name", "library_id", "status"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieVideoCodecs: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_video_codecs"),
			"Tdarr video codecs for library by type",
			[]string{"library_name", "library_id", "codec"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieVideoContainers: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_video_containers"),
			"Tdarr video containers for library by type",
			[]string{"library_name", "library_id", "container_type"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieVideoResolutions: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_video_resolutions"),
			"Tdarr video resolutions for library by type",
			[]string{"library_name", "library_id", "resolution"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieAudioCodecs: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_audio_codecs"),
			"Tdarr audio codecs for library by type",
			[]string{"library_name", "library_id", "codec"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		pieAudioContainers: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_audio_containers"),
			"Tdarr video containers for library by type",
			[]string{"library_name", "library_id", "container_type"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		errorMetric: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "collector_error"),
			"Error while collecting metrics",
			nil,
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		nodeCollector: NewTdarrNodeCollector(runConfig),
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
	ch <- c.nodeCollector.metrics.nodeInfo
	ch <- c.nodeCollector.metrics.nodeUptime
	ch <- c.nodeCollector.metrics.nodeHeapUsedMb
	ch <- c.nodeCollector.metrics.nodeHeapTotalMb
	ch <- c.nodeCollector.metrics.nodeHostCpuPercent
	ch <- c.nodeCollector.metrics.nodeHostMemUsedGb
	ch <- c.nodeCollector.metrics.nodeHostMemTotalGb
	ch <- c.nodeCollector.metrics.nodeWorkerInfo
	ch <- c.nodeCollector.metrics.nodeWorkerFlowInfo
}

func (c *TdarrCollector) httpReqHelper(path string, reqPayload interface{}, target interface{}) error {
	httpClient, err := client.NewRequestClient(c.config.UrlParsed, c.config.VerifySsl, c.config.ApiKey)
	if err != nil {
		log.Error().
			Err(err).Msg("Failed to create http request client for Tdarr, ensure proper URL is provided")
		return err
	}
	log.Debug().Interface("payload", reqPayload).Msg("Requesting statistics data from Tdarr")
	// Marshal it into JSON prior to requesting
	payload, err := json.Marshal(reqPayload)
	if err != nil {
		log.Error().Err(err).Interface("payload", reqPayload).
			Msg("Failed to marshal payload for statistics request")
		return err
	}
	// make request
	httpErr := httpClient.DoPostRequest(path, target, payload)
	if httpErr != nil {
		log.Error().Str("urlPath", path).Interface("payload", reqPayload).Err(httpErr).Msg("Failed to get data for Tdarr exporter")
		return httpErr
	}
	log.Debug().Str("urlPath", path).Interface("payload", reqPayload).Interface("response", target).Msg("Stats API Response")
	return nil
}

// support concurrency
func (c *TdarrCollector) getLibStats(wg *sync.WaitGroup, inChan <-chan TdarrPieDataRequest, outChan chan<- *TdarrPieStats) {
	defer wg.Done()
	for piePayload := range inChan {
		pieMetric := &TdarrPieStats{}
		log.Debug().Interface("payload", piePayload).Msg("Requesting Lib stats pie data from Tdarr")
		err := c.httpReqHelper(c.config.TdarrPieStatsPath, piePayload, pieMetric)
		if err != nil {
			log.Error().Interface("payload", piePayload).Err(err).Msg("Failed to get Lib stats pie data")
			continue
		}
		pieMetric.libraryName = piePayload.Data.libraryName
		pieMetric.libraryId = piePayload.Data.LibraryId
		// if no name, set to all
		if pieMetric.libraryName == "" {
			pieMetric.libraryName = "all"
		}
		// if no id, set to all
		if pieMetric.libraryId == "" {
			pieMetric.libraryId = "all_libraries"
		}
		outChan <- pieMetric
	}
}

func (c *TdarrCollector) Collect(ch chan<- prometheus.Metric) {
	// get server metrics
	metricReqBody := getGeneralReqPayload("")
	metric := &TdarrMetric{}
	err := c.httpReqHelper(c.config.TdarrStatsPath, metricReqBody, &metric)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
		return
	}
	// get metrics data
	var (
		pieData         []*TdarrPieStats
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

	// api changed after v2.24.01+
	if len(metric.Pies) == 0 {
		log.Debug().Msgf("No pie data found in general stats response, attempting to parse via new API `%s`", c.config.TdarrPieStatsPath)
		// get pie data
		overallPie := &TdarrPieStats{}
		// add default "all libraries" to the list
		// if no libraryId is supplied, it should return data combined for all libraries
		overallPayload := TdarrPieDataRequest{
			Data: struct {
				LibraryId   string `json:"libraryId"`
				libraryName string `json:"-"`
			}{
				LibraryId:   "",
				libraryName: "",
			},
		}
		log.Debug().Str("library", "all").Msg("Requesting all lib stats pie data from Tdarr")
		err = c.httpReqHelper(c.config.TdarrPieStatsPath, overallPayload, overallPie)
		if err != nil {
			log.Error().Str("library", "all").Err(err).Msg("Failed to get all lib stats from Tdarr")
			ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
			return
		}

		// check cache for all libraries data
		// get pie data
		shouldCollect := false
		// this won't block other reads when checking
		totalFiles := c.statsCache.GetTotalFiles()
		if totalFiles != overallPie.PieStats.TotalFiles {
			log.Debug().Int("cachedFileCount", totalFiles).Int("apiFileCount", overallPie.PieStats.TotalFiles).Msg("Total files mismatch - gathering metrics")
			c.statsCache.SetTotalFiles(overallPie.PieStats.TotalFiles)
			shouldCollect = true
		}
		// if counts are the same use cache
		if !shouldCollect {
			log.Debug().Msg("Using cached library stats - api file count matches cached value")
			pieData = c.statsCache.GetLibStats()
		}
		// if no data from API returns no file count, nothing to do
		if shouldCollect && overallPie.PieStats.TotalFiles > 0 {
			getLibsPayload := getGeneralReqPayload("library")
			allLibs := []TdarrLibraryInfo{}
			err := c.httpReqHelper(c.config.TdarrStatsPath, getLibsPayload, &allLibs)
			if err != nil {
				log.Error().Err(err).Msg("Failed to get library details")
				ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
				return
			}

			dataWg := &sync.WaitGroup{}
			inChan := make(chan TdarrPieDataRequest, len(allLibs))
			outChan := make(chan *TdarrPieStats, len(allLibs))
			// start workers
			for i := 0; i < c.config.HttpMaxConcurrency; i++ {
				dataWg.Add(1)
				go c.getLibStats(dataWg, inChan, outChan)
			}

			// send data to workers
			for _, lib := range allLibs {
				inChan <- TdarrPieDataRequest{
					Data: struct {
						LibraryId   string `json:"libraryId"`
						libraryName string `json:"-"`
					}{
						LibraryId:   lib.LibraryId,
						libraryName: lib.Name,
					},
				}
			}

			// close channel to signal workers to stop
			close(inChan)

			// wait for workers to finish
			dataWg.Wait()

			// collect results
			resultWg := &sync.WaitGroup{}
			resultWg.Add(1)
			go func() {
				defer resultWg.Done()
				for pie := range outChan {
					pieData = append(pieData, pie)
				}
			}()

			// append all libraries data to the slice
			pieData = append(pieData, overallPie)

			// close channel no longer needed
			close(outChan)

			// wait for results to be collected
			resultWg.Wait()
			log.Debug().Msg("All library stats gathered - setting cache")
			c.statsCache.SetLibStats(pieData)
		}
	} else {
		// old api support
		// have to create a usable struct from tdarr's structured slice response (not an object with keys)
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
		for _, pie := range metric.Pies {
			log.Debug().Interface("pie", pie).Msg("Pie data to be parsed")

			// ensure the data inside each slice adheres to the expected format
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

			// if we don't change, it errors with duplicate label value in single metric with `library_name` label
			libId := pie[1].(string)
			if strings.ToLower(libId) == "all" {
				libId = "all_libraries"
			}

			pieData = append(pieData, &TdarrPieStats{
				libraryName: pie[0].(string),
				libraryId:   libId,
				PieStats: TdarrPieStat{
					// convert to int for now to support old api that is still in usee
					TotalFiles:            int(pie[2].(float64)),
					TotalTranscodeCount:   int(pie[3].(float64)),
					SizeDiff:              pie[4].(float64),
					TotalHealthCheckCount: int(pie[5].(float64)),
					Status: TdarrPieStatusSlice{
						Transcode:   transcodePie,
						HealthCheck: healthPie,
					},
					Video: TdarrPieVideoSlice{
						Codecs:      videoCodecsPie,
						Containers:  videoContainersPie,
						Resolutions: videoResolutionsPie,
					},
					Audio: TdarrPieVideoSlice{
						Codecs:     audioCodecsPie,
						Containers: audioContainersPie,
					},
				},
			})
		}
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
		ch <- prometheus.MustNewConstMetric(c.pieNumFiles, prometheus.GaugeValue, float64(pie.PieStats.TotalFiles), pie.libraryName, pie.libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieNumTranscodes, prometheus.GaugeValue, float64(pie.PieStats.TotalTranscodeCount), pie.libraryName, pie.libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieNumHealthChecks, prometheus.GaugeValue, float64(pie.PieStats.TotalHealthCheckCount), pie.libraryName, pie.libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieSizeDiff, prometheus.GaugeValue, pie.PieStats.SizeDiff, pie.libraryName, pie.libraryId)
		for _, pieSlice := range pie.PieStats.Status.Transcode {
			var labelName string
			statusName := strings.ToLower(pieSlice.Name)
			if strings.HasPrefix(statusName, "transcode") {
				// remove the Transcode prefix
				labelName = cleanUpTranscodeStatus(statusName, true)
			} else {
				labelName = cleanUpTranscodeStatus(statusName, false)
			}
			ch <- prometheus.MustNewConstMetric(c.pieTranscodes, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.libraryName, pie.libraryId, labelName)
		}
		for _, pieSlice := range pie.PieStats.Status.HealthCheck {
			ch <- prometheus.MustNewConstMetric(c.pieHealthChecks, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.libraryName, pie.libraryId, strings.ToLower(pieSlice.Name))
		}
		for _, pieSlice := range pie.PieStats.Video.Codecs {
			ch <- prometheus.MustNewConstMetric(c.pieVideoCodecs, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.libraryName, pie.libraryId, strings.ToLower(pieSlice.Name))
		}
		for _, pieSlice := range pie.PieStats.Video.Containers {
			ch <- prometheus.MustNewConstMetric(c.pieVideoContainers, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.libraryName, pie.libraryId, strings.ToLower(pieSlice.Name))
		}
		for _, pieSlice := range pie.PieStats.Video.Resolutions {
			ch <- prometheus.MustNewConstMetric(c.pieVideoResolutions, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.libraryName, pie.libraryId, strings.ToLower(pieSlice.Name))
		}
		for _, pieSlice := range pie.PieStats.Audio.Codecs {
			ch <- prometheus.MustNewConstMetric(c.pieAudioCodecs, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.libraryName, pie.libraryId, strings.ToLower(pieSlice.Name))
		}
		for _, pieSlice := range pie.PieStats.Audio.Containers {
			ch <- prometheus.MustNewConstMetric(c.pieAudioContainers, prometheus.GaugeValue, float64(pieSlice.Value),
				pie.libraryName, pie.libraryId, strings.ToLower(pieSlice.Name))
		}
	}

	// get all node metrics
	nodeData, err := c.nodeCollector.GetNodeData()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
		return
	}
	// get worker data for each node

	// node data parsing
	for _, node := range nodeData {
		// node info
		ch <- prometheus.MustNewConstMetric(c.nodeCollector.metrics.nodeInfo, prometheus.GaugeValue, 1,
			node.Id, node.Name, node.GpuSelect, strconv.Itoa(node.Priority), strconv.Itoa(node.Config.Pid), strconv.FormatBool(node.Paused),
			strconv.Itoa(node.WorkerLimits.HealthCheckGpu), strconv.Itoa(node.WorkerLimits.HealthCheckCpu),
			strconv.Itoa(node.WorkerLimits.TranscodeGpu), strconv.Itoa(node.WorkerLimits.TranscodeCpu),
			strconv.Itoa(node.QueueLengths.HealthCheckGpu), strconv.Itoa(node.QueueLengths.HealthCheckCpu),
			strconv.Itoa(node.QueueLengths.TranscodeGpu), strconv.Itoa(node.QueueLengths.TranscodeCpu),
		)

		// node uptime
		ch <- prometheus.MustNewConstMetric(c.nodeCollector.metrics.nodeUptime, prometheus.GaugeValue, float64(node.ResourceStats.Process.Uptime),
			node.Id, node.Name)

		// convert resource stats to float from string
		// skip if fail to parse
		log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).Str("heapUsedMb", node.ResourceStats.Process.HeapUsedMb).Msg("Node heap used mb")
		if nodeHeapUsedMb, floatErr := strconv.ParseFloat(node.ResourceStats.Process.HeapUsedMb, 64); floatErr == nil {
			log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).Float64("heapUsedMb", nodeHeapUsedMb).Msg("Node heap information")
			ch <- prometheus.MustNewConstMetric(c.nodeCollector.metrics.nodeHeapUsedMb, prometheus.GaugeValue, nodeHeapUsedMb,
				node.Id, node.Name)
		}
		log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).Str("heapTotalMb", node.ResourceStats.Process.HeapTotalMb).Msg("Node heap total mb")
		if nodeHeapTotalMb, floatErr := strconv.ParseFloat(node.ResourceStats.Process.HeapTotalMb, 64); floatErr == nil {
			ch <- prometheus.MustNewConstMetric(c.nodeCollector.metrics.nodeHeapTotalMb, prometheus.GaugeValue, nodeHeapTotalMb,
				node.Id, node.Name)
		}
		log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).Str("cpuPercent", node.ResourceStats.Os.CpuPercent).Msg("Node cpu percent")
		if nodeHostCpuPercent, floatErr := strconv.ParseFloat(node.ResourceStats.Os.CpuPercent, 64); floatErr == nil {
			ch <- prometheus.MustNewConstMetric(c.nodeCollector.metrics.nodeHostCpuPercent, prometheus.GaugeValue, nodeHostCpuPercent,
				node.Id, node.Name)
		}
		log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).Str("memUsedGb", node.ResourceStats.Os.MemUsedGb).Msg("Node mem used gb")
		if nodeHostMemUsedGb, floatErr := strconv.ParseFloat(node.ResourceStats.Os.MemUsedGb, 64); floatErr == nil {
			ch <- prometheus.MustNewConstMetric(c.nodeCollector.metrics.nodeHostMemUsedGb, prometheus.GaugeValue, nodeHostMemUsedGb,
				node.Id, node.Name)
		}
		log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).Str("memTotalGb", node.ResourceStats.Os.MemTotalGb).Msg("Node mem total gb")
		if nodeHostMemTotalGb, floatErr := strconv.ParseFloat(node.ResourceStats.Os.MemTotalGb, 64); floatErr == nil {
			ch <- prometheus.MustNewConstMetric(c.nodeCollector.metrics.nodeHostMemTotalGb, prometheus.GaugeValue, nodeHostMemTotalGb,
				node.Id, node.Name)
		}

		// node worker info
		for _, worker := range node.Workers {
			log.Debug().Interface("worker", worker).Msg("Worker data")
			// see if flow worker
			// if flow worker then `LastPluginDetails` fields will be empty
			if worker.FlowWorker {
				ch <- prometheus.MustNewConstMetric(c.nodeCollector.metrics.nodeWorkerFlowInfo, prometheus.GaugeValue, 1,
					node.Id, node.Name, worker.Id, worker.WorkerType,
					worker.Status, strconv.FormatInt(worker.StatusTs, 10), strconv.FormatBool(worker.Idle),
					worker.File, strconv.FormatFloat(worker.OriginalfileSizeGb, 'f', -1, 64),
					strconv.Itoa(worker.Fps), worker.Eta,
					strconv.FormatFloat(worker.Percentage, 'f', -1, 64), strconv.FormatBool(worker.Process.Connected), strconv.Itoa(worker.Process.Pid),
					strconv.FormatInt(worker.Job.StartTime, 10), strconv.FormatInt(worker.StartTime, 10),
					strconv.FormatFloat(worker.OutputFileSizeGb, 'f', -1, 64), strconv.FormatFloat(worker.EstSizeGb, 'f', -1, 64),
				)
			} else {
				ch <- prometheus.MustNewConstMetric(c.nodeCollector.metrics.nodeWorkerInfo, prometheus.GaugeValue, 1,
					node.Id, node.Name, worker.Id, worker.WorkerType,
					worker.Status, strconv.FormatInt(worker.StatusTs, 10), strconv.FormatBool(worker.Idle),
					worker.File, strconv.FormatFloat(worker.OriginalfileSizeGb, 'f', -1, 64),
					strconv.Itoa(worker.Fps), worker.Eta,
					strconv.FormatFloat(worker.Percentage, 'f', -1, 64), strconv.FormatBool(worker.Process.Connected), strconv.Itoa(worker.Process.Pid),
					strconv.FormatInt(worker.Job.StartTime, 10), strconv.FormatInt(worker.StartTime, 10),
					worker.LastPluginDetails.Id, worker.LastPluginDetails.PositionNumber,
					strconv.FormatFloat(worker.OutputFileSizeGb, 'f', -1, 64), strconv.FormatFloat(worker.EstSizeGb, 'f', -1, 64),
				)
			}
		}

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

func getGeneralReqPayload(payloadRequestType string) TdarrMetricRequest {
	if payloadRequestType == "library" {
		return TdarrMetricRequest{
			Data: TdarrDataRequest{
				Collection: "LibrarySettingsJSONDB",
				Mode:       "getAll",
				DocId:      "",
				Obj:        map[string]interface{}{},
			},
		}
	} else {
		return TdarrMetricRequest{
			Data: TdarrDataRequest{
				Collection: "StatisticsJSONDB",
				Mode:       "getById",
				DocId:      "statistics",
				Obj:        map[string]interface{}{},
			},
		}
	}
}
