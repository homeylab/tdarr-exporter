package collector

import (
	"encoding/json"
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

// newDesc builds a *prometheus.Desc with the METRIC_PREFIX-prefixed fqName and the
// shared const instance label. It collapses the repeated NewDesc/BuildFQName boilerplate
// in both the collector and node-metrics constructors to a single call per metric.
func newDesc(name, help string, varLabels []string, instance prometheus.Labels) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName(METRIC_PREFIX, "", name), help, varLabels, instance)
}

// tdarrAPI is the HTTP-client seam used by the collectors. *client.RequestClient
// satisfies it directly; tests inject an in-memory fake instead of a real client
// plus httptest server.
type tdarrAPI interface {
	DoRequest(path string, target interface{}, queryParams ...client.QueryParams) error
	DoPostRequest(path string, target interface{}, payload []byte) error
}

// unknownStatusKey identifies a unique (kind, status) pair for the unknown-status counter.
type unknownStatusKey struct {
	kind   string
	status string
}

type TdarrCollector struct {
	config                config.Config
	api                   tdarrAPI // shared HTTP client, built once in the constructor
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
	upMetric              *prometheus.Desc
	// descsList is the collector's own descs in Describe order, assembled once in the
	// constructor. Describe ranges over this plus the node collector's descs(), so a
	// metric is registered for Describe in exactly one place (no field-by-field hand-list).
	descsList []*prometheus.Desc
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
//
// NewTdarrCollector builds the shared HTTP client once from runConfig and wires it
// into both the top-level collector and the embedded node collector. The client is
// surfaced as a tdarrAPI; the error from client.NewRequestClient is propagated so the
// composition root (main) can fail fast on a bad URL.
func NewTdarrCollector(runConfig config.Config) (*TdarrCollector, error) {
	api, err := client.NewRequestClient(runConfig.UrlParsed, runConfig.VerifySsl, runConfig.HttpTimeoutSeconds, runConfig.ApiKey)
	if err != nil {
		log.Error().
			Err(err).Msg("Failed to create http request client for Tdarr, ensure proper URL is provided")
		return nil, err
	}
	return newTdarrCollectorWithAPI(runConfig, api), nil
}

// newTdarrCollectorWithAPI is the test-injection seam: it builds a fully wired
// TdarrCollector around an already-constructed tdarrAPI. The same api is shared with
// the node collector since both hit the same base URL.
func newTdarrCollectorWithAPI(runConfig config.Config, api tdarrAPI) *TdarrCollector {
	instance := prometheus.Labels{"tdarr_instance": runConfig.InstanceName}

	c := &TdarrCollector{
		config:              runConfig,
		api:                 api,
		statsCache:          NewTdarrLibStatsCache(),
		unknownStatusCounts: make(map[unknownStatusKey]float64),
		totalFilesMetric: newDesc(
			"files_total",
			"Tdarr total file count - includes files in ignore lists within each library",
			nil, instance,
		),
		totalTranscodeCount: newDesc(
			"transcodes_total",
			"Tdarr total transcode count for all libraries",
			nil, instance,
		),
		totalHealthCheckCount: newDesc(
			"health_checks_total",
			"Tdarr total health check count for all libraries",
			nil, instance,
		),
		sizeDiff: newDesc(
			"size_diff_gb",
			"Tdarr size difference (+/-) in GB",
			nil, instance,
		),
		tdarrScore: newDesc(
			"score_pct",
			"Tdarr score percentage - how much of your libraries has been handled by tdarr",
			nil, instance,
		),
		healthCheckScore: newDesc(
			"health_check_score_pct",
			"Tdarr health check score percentage - how much of your libraries has been health checked by tdarr",
			nil, instance,
		),
		avgNumStreams: newDesc(
			"avg_num_streams",
			"Tdarr average number of streams in video",
			nil, instance,
		),
		streamStatsDuration: newDesc(
			"stream_stats_duration",
			"Tdarr stream stats duration",
			[]string{"stat_type"}, instance,
		),
		streamStatsBitRate: newDesc(
			"stream_stats_bit_rate",
			"Tdarr stream stats bit rate",
			[]string{"stat_type"}, instance,
		),
		streamStatsNumFrames: newDesc(
			"stream_stats_num_frames",
			"Tdarr stream stats number of frames",
			[]string{"stat_type"}, instance,
		),
		pieNumFiles: newDesc(
			"library_files_total",
			"Tdarr total files in library",
			[]string{"library_name", "library_id"}, instance,
		),
		pieNumTranscodes: newDesc(
			"library_transcodes_total",
			"Tdarr total transcodes for library by status",
			[]string{"library_name", "library_id"}, instance,
		),
		pieNumHealthChecks: newDesc(
			"library_health_checks_total",
			"Tdarr total health checks for library by status",
			[]string{"library_name", "library_id"}, instance,
		),
		pieSizeDiff: newDesc(
			"library_size_diff_gb",
			"Tdarr size difference (+/-) in GB for library",
			[]string{"library_name", "library_id"}, instance,
		),
		pieTranscodes: newDesc(
			"library_transcodes",
			"Tdarr transcodes for library by status",
			[]string{"library_name", "library_id", "status"}, instance,
		),
		pieHealthChecks: newDesc(
			"library_health_checks",
			"Tdarr health checks for library by status",
			[]string{"library_name", "library_id", "status"}, instance,
		),
		pieVideoCodecs: newDesc(
			"library_video_codecs",
			"Tdarr video codecs for library by type",
			[]string{"library_name", "library_id", "codec"}, instance,
		),
		pieVideoContainers: newDesc(
			"library_video_containers",
			"Tdarr video containers for library by type",
			[]string{"library_name", "library_id", "container_type"}, instance,
		),
		pieVideoResolutions: newDesc(
			"library_video_resolutions",
			"Tdarr video resolutions for library by type",
			[]string{"library_name", "library_id", "resolution"}, instance,
		),
		pieAudioCodecs: newDesc(
			"library_audio_codecs",
			"Tdarr audio codecs for library by type",
			[]string{"library_name", "library_id", "codec"}, instance,
		),
		pieAudioContainers: newDesc(
			"library_audio_containers",
			"Tdarr video containers for library by type",
			[]string{"library_name", "library_id", "container_type"}, instance,
		),
		unknownStatusTotal: newDesc(
			"unknown_status_total",
			"Count of pie status values not in the known enum, by job_kind (transcode|healthcheck) and status label. "+
				"A non-zero value indicates Tdarr emitted a status that the exporter does not pre-emit zeros for. "+
				"Use increase(tdarr_unknown_status_total[24h]) > 0 to alert on API drift.",
			[]string{"job_kind", "status"}, instance,
		),
		upMetric: newDesc(
			"up",
			"1 if the last collection cycle succeeded, 0 otherwise (Tdarr API error, response parse error, or partial pie-stats fetch). "+
				"Distinct from prometheus built-in 'up' which indicates exporter process reachability.",
			nil, instance,
		),
		nodeCollector: NewTdarrNodeCollector(runConfig, api),
	}

	// Assemble the collector's own descs once, in Describe order. Describe ranges over
	// this list plus the node collector's descs() — adding a metric means appending here
	// in exactly one place. The order matches the historical hand-written Describe list.
	c.descsList = []*prometheus.Desc{
		c.totalFilesMetric,
		c.totalTranscodeCount,
		c.totalHealthCheckCount,
		c.sizeDiff,
		c.tdarrScore,
		c.healthCheckScore,
		c.pieNumFiles,
		c.pieNumTranscodes,
		c.pieNumHealthChecks,
		c.pieSizeDiff,
		c.pieTranscodes,
		c.pieHealthChecks,
		c.avgNumStreams,
		c.streamStatsDuration,
		c.streamStatsBitRate,
		c.streamStatsNumFrames,
		c.pieVideoCodecs,
		c.pieVideoContainers,
		c.pieVideoResolutions,
		c.pieAudioCodecs,
		c.pieAudioContainers,
		c.unknownStatusTotal,
		c.upMetric,
	}

	return c
}

// Describe emits every registered desc by ranging over a single ordered slice: the
// collector's own descs (assembled in the constructor) followed by the node collector's
// descs(). There is no field-by-field hand-list and no reach-in to nodeCollector.metrics.*,
// so a metric is described in exactly one place. TestDescribe_EmitsAllDescs guards the
// count, since Prometheus does not flag a desc that silently drops out of Describe.
func (c *TdarrCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range append(c.descsList, c.nodeCollector.metrics.descs()...) {
		ch <- d
	}
}

func (c *TdarrCollector) httpReqHelper(path string, reqPayload interface{}, target interface{}) error {
	log.Debug().Interface("payload", reqPayload).Msg("Requesting statistics data from Tdarr")
	// Marshal it into JSON prior to requesting
	payload, err := json.Marshal(reqPayload)
	if err != nil {
		log.Error().Err(err).Interface("payload", reqPayload).
			Msg("Failed to marshal payload for statistics request")
		return err
	}
	// make request
	httpErr := c.api.DoPostRequest(path, target, payload)
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
			// Signal partial failure so Collect() can set tdarr_up=0.
			// Previously-cached normalized data for this library is preserved (acceptable:
			// last-known zero-padded series keep emitting while tdarr_up signals the issue).
			c.partialFailure.Store(true)
			continue
		}
		pieMetric.libraryName = piePayload.Data.libraryName
		pieMetric.libraryId = piePayload.Data.LibraryId
		// Defensive skip: never emit a library series with an empty id or name.
		// The synthetic aggregate sentinel has been removed; use sum() across
		// per-library series in dashboards/queries instead. Tdarr's cruddb
		// response always populates _id and name for real libraries, so this
		// branch should never fire — log loudly if it does.
		if pieMetric.libraryId == "" || pieMetric.libraryName == "" {
			log.Warn().
				Str("libraryId", pieMetric.libraryId).
				Str("libraryName", pieMetric.libraryName).
				Msg("Tdarr returned library with empty id or name; dropping series")
			continue
		}
		// Normalize status slices to cleaned-label maps covering the full known enum.
		// This ensures zero values are emitted for all known statuses even when Tdarr
		// omits them from the response (Tdarr only returns non-zero counts).
		normalizePieStatuses(pieMetric, c.bumpUnknownStatus)
		outChan <- pieMetric
	}
}

// fetchPies fans the per-library pie requests across HttpMaxConcurrency workers and
// fans the results back in. Per-library fetch failures are handled inside getLibStats
// (it sets partialFailure and skips that library), so this returns only the gathered
// pie stats — the same set collect() previously assembled inline before caching.
func (c *TdarrCollector) fetchPies(allLibs []TdarrLibraryInfo) []*TdarrPieStats {
	var pieData []*TdarrPieStats

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
	return pieData
}

func (c *TdarrCollector) Collect(ch chan<- prometheus.Metric) {
	err := c.collect(ch)
	// Always reset partialFailure flag — must NOT short-circuit via OR or the flag leaks across scrapes.
	partial := c.partialFailure.Swap(false)
	v := 1.0
	if err != nil || partial {
		v = 0.0
	}
	ch <- prometheus.MustNewConstMetric(c.upMetric, prometheus.GaugeValue, v)
}

// totalsFromMetric builds the cache-totals snapshot from a general-stats metric.
// Centralizes the field mapping so the totals struct literal lives in exactly one
// place (used by both the cache-write and the refetch comparison).
func totalsFromMetric(metric *TdarrMetric) tdarrCacheTotals {
	return tdarrCacheTotals{
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
	}
}

// shouldRefetch decides whether library pie stats must be re-fetched. It returns
// true when the cache is empty (libStatsNil) or any of the 10 cached totals differs
// from the current metric. tdarrCacheTotals is all-int and thus comparable, so the
// struct != comparison is equivalent to the prior field-by-field OR chain.
func shouldRefetch(cached tdarrCacheTotals, libStatsNil bool, metric *TdarrMetric) bool {
	return libStatsNil || cached != totalsFromMetric(metric)
}

func (c *TdarrCollector) collect(ch chan<- prometheus.Metric) error {
	// get server metrics
	metricReqBody := getGeneralReqPayload("")
	metric := &TdarrMetric{}
	err := c.httpReqHelper(c.config.TdarrStatsPath, metricReqBody, &metric)
	if err != nil {
		return err
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
		return floatConvertErr
	}
	healthScore, floatConvertErr = strconv.ParseFloat(metric.HealthCheckScore, 64)
	if floatConvertErr != nil {
		log.Error().Str("healthCodeStr", metric.HealthCheckScore).Err(floatConvertErr).Msg("Failed to convert a health score to float")
		return floatConvertErr
	}

	// supports only api versions: v2.24.01+
	log.Debug().Str("path", c.config.TdarrPieStatsPath).Msg("Fetching library pie stats")
	// already have total file count from general stats (`metric.TotalFileCount`)
	// check cache for all libraries data
	// this won't block other reads when checking
	cacheTotals := c.statsCache.GetTotals()
	// Refetch if the cache is empty (first run) or any of the 10 totals changed.
	// table0Count–table6Count cover every per-bucket state transition (e.g. files queued
	// but not yet transcoded), catching invalidation cases the three top-level counts miss.
	// Older Tdarr versions that omit tableXCount fields decode to 0; 0==0 means no spurious
	// refetches — behavior degrades gracefully to original 3-totals logic.
	shouldCollect := shouldRefetch(cacheTotals, c.statsCache.GetLibStats() == nil, metric)
	if shouldCollect {
		log.Debug().Msg("Stats totals mismatch - re-fetching library pie stats")
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
			return err
		}

		pieData = c.fetchPies(allLibs)
		log.Debug().Msg("All library stats gathered - setting cache")
		c.statsCache.SetLibStats(pieData)

		// set totals here after all data is collected
		c.statsCache.SetTotals(totalsFromMetric(metric))
	}

	// add metrics to collector
	c.emitGeneralMetrics(ch, metric, score, healthScore)
	c.emitPieMetrics(ch, pieData)

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
		return err
	}
	// get worker data for each node
	c.emitNodeMetrics(ch, nodeData)
	return nil
}

// emitGeneralMetrics emits the top-level server gauges and stream-stats series for a
// general-stats metric. Pure: it only reads the metric/scores and writes to ch.
func (c *TdarrCollector) emitGeneralMetrics(ch chan<- prometheus.Metric, metric *TdarrMetric, score, healthScore float64) {
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
}

// emitPieSlices emits one gauge per slice in a pie-slice list (codecs/containers/resolutions),
// lowercasing the slice name as the final label. Shared by the five video/audio loops.
func emitPieSlices(ch chan<- prometheus.Metric, desc *prometheus.Desc, libName, libId string, slices []TdarrPieSlice) {
	for _, pieSlice := range slices {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(pieSlice.Value),
			libName, libId, strings.ToLower(pieSlice.Name))
	}
}

// emitPieMetrics emits the per-library pie series (totals, normalized status maps, and the
// video/audio codec/container/resolution slices). Pure: reads pieData, writes to ch.
func (c *TdarrCollector) emitPieMetrics(ch chan<- prometheus.Metric, pieData []*TdarrPieStats) {
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
		emitPieSlices(ch, c.pieVideoCodecs, pie.libraryName, pie.libraryId, pie.PieStats.Video.Codecs)
		emitPieSlices(ch, c.pieVideoContainers, pie.libraryName, pie.libraryId, pie.PieStats.Video.Containers)
		emitPieSlices(ch, c.pieVideoResolutions, pie.libraryName, pie.libraryId, pie.PieStats.Video.Resolutions)
		emitPieSlices(ch, c.pieAudioCodecs, pie.libraryName, pie.libraryId, pie.PieStats.Audio.Codecs)
		emitPieSlices(ch, c.pieAudioContainers, pie.libraryName, pie.libraryId, pie.PieStats.Audio.Containers)
	}
}

// emitParsedFloat parses raw as a float64 and, on success, emits a node-scoped gauge.
// On parse failure it debug-logs and silently skips the metric (intentional: Tdarr may
// send empty/non-numeric resource strings for nodes that haven't reported yet).
func emitParsedFloat(ch chan<- prometheus.Metric, desc *prometheus.Desc, raw string, nodeId, nodeName string) {
	if v, floatErr := strconv.ParseFloat(raw, 64); floatErr == nil {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v, nodeId, nodeName)
	} else {
		log.Debug().Str("nodeId", nodeId).Str("nodeName", nodeName).
			Str("raw", raw).Err(floatErr).Msg("Failed to parse node resource stat; skipping metric")
	}
}

// emitNodeMetrics emits all per-node and per-worker series for the given node map.
// Pure: reads nodeData, writes to ch. Resource-stat parse failures are silently skipped
// (see emitParsedFloat); ETA parse failures skip only the eta_seconds gauge.
func (c *TdarrCollector) emitNodeMetrics(ch chan<- prometheus.Metric, nodeData map[string]TdarrNode) {
	for _, node := range nodeData {
		m := c.nodeCollector.metrics

		// node identity info
		ch <- prometheus.MustNewConstMetric(m.nodeInfo, prometheus.GaugeValue, 1,
			node.Id, node.Name, node.GpuSelect,
			strconv.Itoa(node.Config.Pid), strconv.Itoa(node.Priority),
			strconv.FormatBool(node.AllowGpuDoCpu),
		)

		// node uptime
		ch <- prometheus.MustNewConstMetric(m.nodeUptime, prometheus.GaugeValue,
			float64(node.ResourceStats.Process.Uptime), node.Id, node.Name)

		// convert resource stats to float from string; skip on parse failure
		emitParsedFloat(ch, m.nodeHeapUsedMb, node.ResourceStats.Process.HeapUsedMb, node.Id, node.Name)
		emitParsedFloat(ch, m.nodeHeapTotalMb, node.ResourceStats.Process.HeapTotalMb, node.Id, node.Name)
		emitParsedFloat(ch, m.nodeHostCpuPercent, node.ResourceStats.Os.CpuPercent, node.Id, node.Name)
		emitParsedFloat(ch, m.nodeHostMemUsedGb, node.ResourceStats.Os.MemUsedGb, node.Id, node.Name)
		emitParsedFloat(ch, m.nodeHostMemTotalGb, node.ResourceStats.Os.MemTotalGb, node.Id, node.Name)

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

		// worker count by type — count from active workers map.
		// Always emit zeros for the four known dims; emit unknown buckets only when non-zero
		// (raw API string preserved as worker_type, "unknown" as compute_type).
		workerCounts := countWorkersByType(node.Workers)
		for _, d := range knownWorkerTypeDims {
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerCount, prometheus.GaugeValue,
				float64(workerCounts.known[d]), node.Id, node.Name, d.workerType, d.computeType)
		}
		for rawType, count := range workerCounts.unknown {
			if count == 0 {
				continue
			}
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerCount, prometheus.GaugeValue,
				float64(count), node.Id, node.Name, rawType, computeTypeUnknown)
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

			// unified worker info metric (all workers, flow or classic).
			// Split Tdarr's compound workerType string into worker_type + compute_type labels.
			wType, cType := parseWorkerType(worker.WorkerType)
			ch <- prometheus.MustNewConstMetric(m.nodeWorkerInfo, prometheus.GaugeValue, 1,
				node.Id, node.Name, worker.Id, wType, cType,
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
