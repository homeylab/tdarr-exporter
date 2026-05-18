package collector

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/homeylab/tdarr-exporter/internal/client"
	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

var (
	METRIC_PREFIX = "tdarr"
)

// unknownStatusKey identifies a unique (kind, status) pair for the unknown-status counter.
type unknownStatusKey struct {
	kind   string
	status string
}

type TdarrCollector struct {
	config                config.Config
	statsCache            *TdarrLibStatsCache
	partialFailure        atomic.Bool // set by getLibStats workers on per-library fetch error
	unknownStatusMu       sync.Mutex
	unknownStatusCounts   map[unknownStatusKey]float64 // monotonic counter for enum drift detection
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
	unknownStatusTotal    *prometheus.Desc    // counter for status values not in known enum
	nodeCollector         *TdarrNodeCollector // node data
	errorMetric           *prometheus.Desc    // Error Description for use with InvalidMetric
}

// Cache to store library stats and reduce excessive API calls
// Mutex added to reduce chance of running into errors (from race condition) from misconfiguration or manual testing
// i.e getting scraped twice by two different prometheus instances
type TdarrLibStatsCache struct {
	mu           sync.RWMutex
	totals       tdarrCacheTotals
	libraryStats []*TdarrPieStats
}

func NewTdarrLibStatsCache() *TdarrLibStatsCache {
	return &TdarrLibStatsCache{
		totals:       tdarrCacheTotals{},
		libraryStats: nil,
	}
}

func (c *TdarrLibStatsCache) GetTotals() tdarrCacheTotals {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totals
}

func (c *TdarrLibStatsCache) SetTotals(totals tdarrCacheTotals) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totals = totals
}

func (c *TdarrLibStatsCache) GetLibStats() []*TdarrPieStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.libraryStats
}

func (c *TdarrLibStatsCache) SetLibStats(stats []*TdarrPieStats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.libraryStats = stats
}

// collector
func NewTdarrCollector(runConfig config.Config) *TdarrCollector {
	return &TdarrCollector{
		config:              runConfig,
		statsCache:          NewTdarrLibStatsCache(),
		unknownStatusCounts: make(map[unknownStatusKey]float64),
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
		unknownStatusTotal: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "unknown_status_total"),
			"Count of pie status values not in the known enum, by kind (transcode|healthcheck) and status label. "+
				"A non-zero value indicates Tdarr emitted a status that the exporter does not pre-emit zeros for. "+
				"Use increase(tdarr_unknown_status_total[24h]) > 0 to alert on API drift.",
			[]string{"kind", "status"},
			prometheus.Labels{"tdarr_instance": runConfig.InstanceName},
		),
		errorMetric: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "collector_error"),
			"1 if exporter failed to fetch from Tdarr API on last scrape (including partial pie-stats failures), "+
				"0 otherwise. Distinct from prometheus 'up' metric which indicates exporter reachability.",
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
	ch <- c.streamStatsNumFrames
	ch <- c.pieVideoCodecs
	ch <- c.pieVideoContainers
	ch <- c.pieVideoResolutions
	ch <- c.pieAudioCodecs
	ch <- c.pieAudioContainers
	ch <- c.unknownStatusTotal
	ch <- c.nodeCollector.metrics.nodeInfo
	ch <- c.nodeCollector.metrics.nodeUptime
	ch <- c.nodeCollector.metrics.nodeHeapUsedMb
	ch <- c.nodeCollector.metrics.nodeHeapTotalMb
	ch <- c.nodeCollector.metrics.nodeHostCpuPercent
	ch <- c.nodeCollector.metrics.nodeHostMemUsedGb
	ch <- c.nodeCollector.metrics.nodeHostMemTotalGb
	ch <- c.nodeCollector.metrics.nodePaused
	ch <- c.nodeCollector.metrics.nodeMaxGpuWorkers
	ch <- c.nodeCollector.metrics.nodeScheduleEnabled
	ch <- c.nodeCollector.metrics.nodeWorkerCount
	ch <- c.nodeCollector.metrics.nodeWorkerLimit
	ch <- c.nodeCollector.metrics.nodeQueueLength
	ch <- c.nodeCollector.metrics.nodeWorkerInfo
	ch <- c.nodeCollector.metrics.nodeWorkerPercentage
	ch <- c.nodeCollector.metrics.nodeWorkerFps
	ch <- c.nodeCollector.metrics.nodeWorkerOriginalFileSizeGb
	ch <- c.nodeCollector.metrics.nodeWorkerOutputFileSizeGb
	ch <- c.nodeCollector.metrics.nodeWorkerEstFileSizeGb
	ch <- c.nodeCollector.metrics.nodeWorkerJobStartTimestamp
	ch <- c.nodeCollector.metrics.nodeWorkerStartTimestamp
	ch <- c.nodeCollector.metrics.nodeWorkerStatusTimestamp
	ch <- c.nodeCollector.metrics.nodeWorkerEtaSeconds
	ch <- c.nodeCollector.metrics.nodeWorkerPid
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

// bumpUnknownStatus increments the persistent unknown-status counter for the given kind and status.
// Safe for concurrent use from multiple getLibStats goroutines.
func (c *TdarrCollector) bumpUnknownStatus(kind, status string) {
	key := unknownStatusKey{kind: kind, status: status}
	c.unknownStatusMu.Lock()
	c.unknownStatusCounts[key]++
	c.unknownStatusMu.Unlock()
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
			// Signal partial failure so Collect() can emit tdarr_collector_error.
			// Previously-cached normalized data for this library is preserved (acceptable:
			// last-known zero-padded series keep emitting while tdarr_collector_error signals the issue).
			c.partialFailure.Store(true)
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
		// Normalize status slices to cleaned-label maps covering the full known enum.
		// This ensures zero values are emitted for all known statuses even when Tdarr
		// omits them from the response (Tdarr only returns non-zero counts).
		normalizePieStatuses(pieMetric, c.bumpUnknownStatus)
		outChan <- pieMetric
	}
}

func (c *TdarrCollector) Collect(ch chan<- prometheus.Metric) {
	// Reset partialFailure flag at end of every scrape regardless of code path.
	// Flag is set by getLibStats workers during pie fan-out. Defer guarantees the
	// flag never leaks across scrapes even if early returns skip the in-path reset.
	defer func() {
		if c.partialFailure.Swap(false) {
			ch <- prometheus.NewInvalidMetric(c.errorMetric, errors.New("partial pie fetch failure: one or more library stats could not be retrieved"))
		}
	}()

	// get server metrics
	metricReqBody := getGeneralReqPayload("")
	metric := &TdarrMetric{}
	err := c.httpReqHelper(c.config.TdarrStatsPath, metricReqBody, &metric)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
		return
	}

	log.Debug().Int("totalFiles", metric.TotalFileCount).
		Int("totalTranscodes", metric.TotalTranscodeCount).
		Int("totalHealthChecks", metric.TotalHealthCheckCount).
		Msg("General stats totals")

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

	// supports only api versions: v2.24.01+
	log.Debug().Str("path", c.config.TdarrPieStatsPath).Msg("Fetching library pie stats")
	// already have total file count from general stats (`metric.TotalFileCount`)
	// check cache for all libraries data
	shouldCollect := false

	// this won't block other reads when checking
	cacheTotals := c.statsCache.GetTotals()
	// if total counts has changed from cache and cache is populated, need to re-collect
	// also collect if cache is empty (first run)
	// Compare all 10 totals. table0Count–table6Count cover every per-bucket state transition
	// (e.g. files queued but not yet transcoded), catching invalidation cases the three top-level
	// counts miss. Older Tdarr versions that omit tableXCount fields decode to 0; 0==0 means
	// no spurious refetches — behavior degrades gracefully to original 3-totals logic.
	if c.statsCache.libraryStats == nil ||
		cacheTotals.totalFileCount != metric.TotalFileCount ||
		cacheTotals.totalTranscodeCount != metric.TotalTranscodeCount ||
		cacheTotals.totalHealthCheckCount != metric.TotalHealthCheckCount ||
		cacheTotals.holdQueue != metric.HoldQueue ||
		cacheTotals.transcodeQueue != metric.TranscodeQueue ||
		cacheTotals.transcodeSuccess != metric.TranscodeSuccess ||
		cacheTotals.transcodeFailed != metric.TranscodeFailed ||
		cacheTotals.healthCheckQueue != metric.HealthCheckQueue ||
		cacheTotals.healthCheckSuccess != metric.HealthCheckSuccess ||
		cacheTotals.healthCheckFailed != metric.HealthCheckFailed {
		log.Debug().Msg("Stats totals mismatch - re-fetching library pie stats")
		shouldCollect = true
	}
	// if counts are the same use cache
	if !shouldCollect {
		log.Debug().Msg("Using cached library stats - api totals matches cached values")
		pieData = c.statsCache.GetLibStats()
	} else { // fetch new data and update cache
		getLibsPayload := getGeneralReqPayload("library")
		allLibs := []TdarrLibraryInfo{}
		err := c.httpReqHelper(c.config.TdarrStatsPath, getLibsPayload, &allLibs)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get library details")
			ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
			return
		}

		// add for default all lib stats
		allLibs = append(allLibs, TdarrLibraryInfo{
			LibraryId: "",
			Name:      "",
		})

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

		// close channel no longer needed
		close(outChan)

		// wait for results to be collected
		resultWg.Wait()
		log.Debug().Msg("All library stats gathered - setting cache")
		c.statsCache.SetLibStats(pieData)

		// set totals here after all data is collected
		c.statsCache.SetTotals(tdarrCacheTotals{
			totalFileCount:        metric.TotalFileCount,
			totalTranscodeCount:   metric.TotalTranscodeCount,
			totalHealthCheckCount: metric.TotalHealthCheckCount,
			holdQueue:             metric.HoldQueue,
			transcodeQueue:        metric.TranscodeQueue,
			transcodeSuccess:      metric.TranscodeSuccess,
			transcodeFailed:       metric.TranscodeFailed,
			healthCheckQueue:      metric.HealthCheckQueue,
			healthCheckSuccess:    metric.HealthCheckSuccess,
			healthCheckFailed:     metric.HealthCheckFailed,
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
		ch <- prometheus.MustNewConstMetric(c.pieNumFiles, prometheus.GaugeValue, float64(pie.PieStats.TotalFiles), pie.libraryName, pie.libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieNumTranscodes, prometheus.GaugeValue, float64(pie.PieStats.TotalTranscodeCount), pie.libraryName, pie.libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieNumHealthChecks, prometheus.GaugeValue, float64(pie.PieStats.TotalHealthCheckCount), pie.libraryName, pie.libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieSizeDiff, prometheus.GaugeValue, pie.PieStats.SizeDiff, pie.libraryName, pie.libraryId)
		// Emit transcode statuses from the normalized map (pre-cleaned labels, full enum coverage).
		for status, count := range pie.NormalizedTranscodes {
			ch <- prometheus.MustNewConstMetric(c.pieTranscodes, prometheus.GaugeValue, float64(count),
				pie.libraryName, pie.libraryId, status)
		}
		// Emit health check statuses from the normalized map (pre-cleaned labels, full enum coverage).
		for status, count := range pie.NormalizedHealthChecks {
			ch <- prometheus.MustNewConstMetric(c.pieHealthChecks, prometheus.GaugeValue, float64(count),
				pie.libraryName, pie.libraryId, status)
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

	// Emit unknown-status counters (monotonically increasing across scrapes).
	// A non-zero value means Tdarr returned a status label the exporter did not pre-init zeros for.
	c.unknownStatusMu.Lock()
	for key, count := range c.unknownStatusCounts {
		ch <- prometheus.MustNewConstMetric(c.unknownStatusTotal, prometheus.CounterValue, count,
			key.kind, key.status)
	}
	c.unknownStatusMu.Unlock()

	// get all node metrics
	nodeData, err := c.nodeCollector.GetNodeData()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
		return
	}
	// get worker data for each node

	// node data parsing
	for _, node := range nodeData {
		m := c.nodeCollector.metrics

		// node identity info
		ch <- prometheus.MustNewConstMetric(m.nodeInfo, prometheus.GaugeValue, 1,
			node.Id, node.Name, node.GpuSelect,
			strconv.Itoa(node.Config.Pid), strconv.Itoa(node.Priority),
			strconv.FormatBool(node.AllowGpuDoCpu), strconv.FormatBool(node.Paused),
		)

		// node uptime
		ch <- prometheus.MustNewConstMetric(m.nodeUptime, prometheus.GaugeValue,
			float64(node.ResourceStats.Process.Uptime), node.Id, node.Name)

		// convert resource stats to float from string; skip on parse failure
		log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).
			Str("heapUsedMb", node.ResourceStats.Process.HeapUsedMb).Msg("Node heap used mb")
		if v, floatErr := strconv.ParseFloat(node.ResourceStats.Process.HeapUsedMb, 64); floatErr == nil {
			ch <- prometheus.MustNewConstMetric(m.nodeHeapUsedMb, prometheus.GaugeValue, v, node.Id, node.Name)
		}
		log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).
			Str("heapTotalMb", node.ResourceStats.Process.HeapTotalMb).Msg("Node heap total mb")
		if v, floatErr := strconv.ParseFloat(node.ResourceStats.Process.HeapTotalMb, 64); floatErr == nil {
			ch <- prometheus.MustNewConstMetric(m.nodeHeapTotalMb, prometheus.GaugeValue, v, node.Id, node.Name)
		}
		log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).
			Str("cpuPercent", node.ResourceStats.Os.CpuPercent).Msg("Node cpu percent")
		if v, floatErr := strconv.ParseFloat(node.ResourceStats.Os.CpuPercent, 64); floatErr == nil {
			ch <- prometheus.MustNewConstMetric(m.nodeHostCpuPercent, prometheus.GaugeValue, v, node.Id, node.Name)
		}
		log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).
			Str("memUsedGb", node.ResourceStats.Os.MemUsedGb).Msg("Node mem used gb")
		if v, floatErr := strconv.ParseFloat(node.ResourceStats.Os.MemUsedGb, 64); floatErr == nil {
			ch <- prometheus.MustNewConstMetric(m.nodeHostMemUsedGb, prometheus.GaugeValue, v, node.Id, node.Name)
		}
		log.Debug().Str("nodeId", node.Id).Str("nodeName", node.Name).
			Str("memTotalGb", node.ResourceStats.Os.MemTotalGb).Msg("Node mem total gb")
		if v, floatErr := strconv.ParseFloat(node.ResourceStats.Os.MemTotalGb, 64); floatErr == nil {
			ch <- prometheus.MustNewConstMetric(m.nodeHostMemTotalGb, prometheus.GaugeValue, v, node.Id, node.Name)
		}

		// node state gauges
		pausedVal := 0.0
		if node.Paused {
			pausedVal = 1.0
		}
		ch <- prometheus.MustNewConstMetric(m.nodePaused, prometheus.GaugeValue, pausedVal, node.Id, node.Name)
		ch <- prometheus.MustNewConstMetric(m.nodeMaxGpuWorkers, prometheus.GaugeValue, float64(node.MaxGpuWorkers), node.Id, node.Name)
		schedVal := 0.0
		if node.ScheduleEnabled {
			schedVal = 1.0
		}
		ch <- prometheus.MustNewConstMetric(m.nodeScheduleEnabled, prometheus.GaugeValue, schedVal, node.Id, node.Name)

		// per-type gauges — always emit all four types so zero-value series appear
		emitPerType(ch, m.nodeWorkerLimit, node.Id, node.Name, node.WorkerLimits)
		emitPerType(ch, m.nodeQueueLength, node.Id, node.Name, node.QueueLengths)

		// worker count by type — count from active workers map
		workerCounts := countWorkersByType(node.Workers)
		for _, wType := range knownWorkerTypes {
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerCount, prometheus.GaugeValue,
				float64(workerCounts[wType]), node.Id, node.Name, wType)
		}
		// emit unknown bucket only if non-zero to avoid polluting metric with permanent zero
		if unknownCount, hasUnknown := workerCounts[workerTypeUnknown]; hasUnknown && unknownCount > 0 {
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerCount, prometheus.GaugeValue,
				float64(unknownCount), node.Id, node.Name, workerTypeUnknown)
		}

		// per-worker metrics
		for _, worker := range node.Workers {
			log.Debug().Interface("worker", worker).Msg("Worker data")

			// plugin labels: empty strings for flow workers (no plugin step concept)
			pluginId := worker.LastPluginDetails.Id
			pluginPosition := worker.LastPluginDetails.PositionNumber
			if worker.FlowWorker {
				pluginId = ""
				pluginPosition = ""
			}

			// unified worker info metric (all workers, flow or classic)
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerInfo, prometheus.GaugeValue, 1,
				node.Id, node.Name, worker.Id, worker.WorkerType,
				strconv.FormatBool(worker.FlowWorker),
				worker.Status, worker.File,
				pluginId, pluginPosition,
				strconv.FormatBool(worker.Process.Connected), strconv.FormatBool(worker.Idle),
			)

			// per-worker numeric gauges
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerPercentage, prometheus.GaugeValue,
				worker.Percentage, node.Id, node.Name, worker.Id)
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerFps, prometheus.GaugeValue,
				float64(worker.Fps), node.Id, node.Name, worker.Id)
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerOriginalFileSizeGb, prometheus.GaugeValue,
				worker.OriginalfileSizeGb, node.Id, node.Name, worker.Id)
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerOutputFileSizeGb, prometheus.GaugeValue,
				worker.OutputFileSizeGb, node.Id, node.Name, worker.Id)
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerEstFileSizeGb, prometheus.GaugeValue,
				worker.EstSizeGb, node.Id, node.Name, worker.Id)
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerJobStartTimestamp, prometheus.GaugeValue,
				float64(worker.Job.StartTime), node.Id, node.Name, worker.Id)
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerStartTimestamp, prometheus.GaugeValue,
				float64(worker.StartTime), node.Id, node.Name, worker.Id)
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerStatusTimestamp, prometheus.GaugeValue,
				float64(worker.StatusTs), node.Id, node.Name, worker.Id)
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerPid, prometheus.GaugeValue,
				float64(worker.Process.Pid), node.Id, node.Name, worker.Id)

			// ETA: parse "H:MM:SS" string into seconds; skip on parse failure
			if etaSecs, ok := parseEtaSeconds(worker.Eta); ok {
				ch <- prometheus.MustNewConstMetric(m.nodeWorkerEtaSeconds, prometheus.GaugeValue,
					float64(etaSecs), node.Id, node.Name, worker.Id)
			} else {
				log.Debug().Str("nodeId", node.Id).Str("workerId", worker.Id).
					Str("eta", worker.Eta).Msg("Failed to parse worker ETA; skipping metric")
			}
		}
	}
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
