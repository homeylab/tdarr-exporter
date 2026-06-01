package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/homeylab/tdarr-exporter/internal/client"
	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const METRIC_PREFIX = "tdarr"

// Sentinel categories for collection failures so callers and tests can branch
// on the cause with errors.Is instead of matching error strings. Boundary errors
// wrap one of these alongside the underlying cause (via multi-%w), so the original
// error chain (e.g. a transport or JSON error) stays inspectable.
var (
	// ErrUpstream marks a failure talking to the Tdarr API: request construction,
	// transport, a non-2xx status, or an unreadable/undecodable response body.
	ErrUpstream = errors.New("tdarr upstream request failed")
	// ErrParse marks a failure interpreting an otherwise-successful response:
	// payload marshalling or a numeric field that could not be converted.
	ErrParse = errors.New("tdarr response parse failed")
)

// buildDesc builds a *prometheus.Desc with the METRIC_PREFIX-prefixed fqName and the
// shared const instance label. It collapses the repeated NewDesc/BuildFQName boilerplate
// in both the collector and node-metrics constructors to a single call per metric.
func buildDesc(name, help string, varLabels []string, instance prometheus.Labels) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName(METRIC_PREFIX, "", name), help, varLabels, instance)
}

// typedDesc bundles a *prometheus.Desc with its value type so emit sites don't have
// to repeat prometheus.GaugeValue/CounterValue on every MustNewConstMetric call. The
// value type lives next to the desc in exactly one place (the constructor).
type typedDesc struct {
	desc      *prometheus.Desc
	valueType prometheus.ValueType
}

// mustNewConstMetric emits a const metric for this desc using its bundled value type.
func (d typedDesc) mustNewConstMetric(value float64, labelValues ...string) prometheus.Metric {
	return prometheus.MustNewConstMetric(d.desc, d.valueType, value, labelValues...)
}

// newGauge / newCounter build a typedDesc carrying the matching Prometheus value type.
func newGauge(name, help string, varLabels []string, instance prometheus.Labels) typedDesc {
	return typedDesc{desc: buildDesc(name, help, varLabels, instance), valueType: prometheus.GaugeValue}
}

func newCounter(name, help string, varLabels []string, instance prometheus.Labels) typedDesc {
	return typedDesc{desc: buildDesc(name, help, varLabels, instance), valueType: prometheus.CounterValue}
}

// tdarrAPI is the HTTP-client seam used by the collectors. *client.RequestClient
// satisfies it directly; tests inject an in-memory fake instead of a real client
// plus httptest server.
type tdarrAPI interface {
	DoRequest(ctx context.Context, path string, target any, queryParams ...client.QueryParams) error
	DoPostRequest(ctx context.Context, path string, target any, payload []byte) error
}

// unknownStatusKey identifies a unique (kind, status) pair for the unknown-status counter.
type unknownStatusKey struct {
	kind   string
	status string
}

type TdarrCollector struct {
	// Only the config values read at collect time are stored, not the whole
	// config.Config bag — the URL/SSL/timeout/api-key and instance label are
	// consumed once in the constructor (client + descs) and never needed again.
	statsPath      string
	pieStatsPath   string
	maxConcurrency int
	api            tdarrAPI // shared HTTP client, built once in the constructor
	// baseCtx is the parent context for every scrape's HTTP requests. main wires in
	// a context cancelled on shutdown so in-flight scrapes abort promptly; tests and
	// the WithAPI constructor default it to context.Background().
	baseCtx context.Context
	// logger defaults to the package-global log.Logger and is shared with the node
	// collector at construction. Injected so tests can silence or capture logs.
	logger                zerolog.Logger
	statsCache            *TdarrLibStatsCache
	partialFailure        atomic.Bool // set by getLibStats workers on per-library fetch error
	unknownStatusMu       sync.Mutex
	unknownStatusCounts   map[unknownStatusKey]float64 // monotonic counter for enum drift detection
	totalFilesMetric      typedDesc
	totalTranscodeCount   typedDesc
	totalHealthCheckCount typedDesc
	sizeDiff              typedDesc
	tdarrScore            typedDesc
	healthCheckScore      typedDesc
	avgNumStreams         typedDesc
	streamStatsDuration   typedDesc
	streamStatsBitRate    typedDesc
	streamStatsNumFrames  typedDesc
	pieNumFiles           typedDesc
	pieNumTranscodes      typedDesc
	pieNumHealthChecks    typedDesc
	pieSizeDiff           typedDesc
	pieTranscodes         typedDesc
	pieHealthChecks       typedDesc
	pieVideoCodecs        typedDesc
	pieVideoContainers    typedDesc
	pieVideoResolutions   typedDesc
	pieAudioCodecs        typedDesc
	pieAudioContainers    typedDesc
	pieAudioResolutions   typedDesc
	unknownStatusTotal    typedDesc           // counter for status values not in known enum
	nodeCollector         *TdarrNodeCollector // node data
	upMetric              typedDesc
	// descsList is the collector's own descs in Describe order, assembled once in the
	// constructor. Describe ranges over this plus the node collector's descs(), so a
	// metric is registered for Describe in exactly one place (no field-by-field hand-list).
	descsList []typedDesc
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
func NewTdarrCollector(ctx context.Context, runConfig config.Config) (*TdarrCollector, error) {
	api, err := client.NewRequestClient(runConfig.UrlParsed, runConfig.VerifySsl, runConfig.HttpTimeoutSeconds, runConfig.ApiKey)
	if err != nil {
		log.Error().
			Err(err).Msg("Failed to create http request client for Tdarr, ensure proper URL is provided")
		return nil, err
	}
	c := newTdarrCollectorWithAPI(runConfig, api)
	// Wire the shutdown-cancellable context from the composition root so a scrape
	// in flight when the process is terminating aborts instead of running to completion.
	c.baseCtx = ctx
	return c, nil
}

// newTdarrCollectorWithAPI is the test-injection seam: it builds a fully wired
// TdarrCollector around an already-constructed tdarrAPI. The same api is shared with
// the node collector since both hit the same base URL.
func newTdarrCollectorWithAPI(runConfig config.Config, api tdarrAPI) *TdarrCollector {
	instance := prometheus.Labels{"tdarr_instance": runConfig.InstanceName}

	c := &TdarrCollector{
		statsPath:           runConfig.TdarrStatsPath,
		pieStatsPath:        runConfig.TdarrPieStatsPath,
		maxConcurrency:      runConfig.HttpMaxConcurrency,
		api:                 api,
		baseCtx:             context.Background(),
		logger:              log.Logger,
		statsCache:          NewTdarrLibStatsCache(),
		unknownStatusCounts: make(map[unknownStatusKey]float64),
		totalFilesMetric: newGauge(
			"files_total",
			"Tdarr total file count - includes files in ignore lists within each library",
			nil, instance,
		),
		totalTranscodeCount: newGauge(
			"transcodes_total",
			"Tdarr total transcode count for all libraries",
			nil, instance,
		),
		totalHealthCheckCount: newGauge(
			"health_checks_total",
			"Tdarr total health check count for all libraries",
			nil, instance,
		),
		sizeDiff: newGauge(
			"size_diff_gb",
			"Tdarr size difference (+/-) in GB",
			nil, instance,
		),
		tdarrScore: newGauge(
			"score_pct",
			"Tdarr score percentage - how much of your libraries has been handled by tdarr",
			nil, instance,
		),
		healthCheckScore: newGauge(
			"health_check_score_pct",
			"Tdarr health check score percentage - how much of your libraries has been health checked by tdarr",
			nil, instance,
		),
		avgNumStreams: newGauge(
			"avg_num_streams",
			"Tdarr average number of streams in video",
			nil, instance,
		),
		streamStatsDuration: newGauge(
			"stream_stats_duration",
			"Tdarr stream stats duration",
			[]string{"stat_type"}, instance,
		),
		streamStatsBitRate: newGauge(
			"stream_stats_bit_rate",
			"Tdarr stream stats bit rate",
			[]string{"stat_type"}, instance,
		),
		streamStatsNumFrames: newGauge(
			"stream_stats_num_frames",
			"Tdarr stream stats number of frames",
			[]string{"stat_type"}, instance,
		),
		pieNumFiles: newGauge(
			"library_files_total",
			"Tdarr total files in library",
			[]string{"library_name", "library_id"}, instance,
		),
		pieNumTranscodes: newGauge(
			"library_transcodes_total",
			"Tdarr total transcodes for library by status",
			[]string{"library_name", "library_id"}, instance,
		),
		pieNumHealthChecks: newGauge(
			"library_health_checks_total",
			"Tdarr total health checks for library by status",
			[]string{"library_name", "library_id"}, instance,
		),
		pieSizeDiff: newGauge(
			"library_size_diff_gb",
			"Tdarr size difference (+/-) in GB for library",
			[]string{"library_name", "library_id"}, instance,
		),
		pieTranscodes: newGauge(
			"library_transcodes",
			"Tdarr transcodes for library by status",
			[]string{"library_name", "library_id", "status"}, instance,
		),
		pieHealthChecks: newGauge(
			"library_health_checks",
			"Tdarr health checks for library by status",
			[]string{"library_name", "library_id", "status"}, instance,
		),
		pieVideoCodecs: newGauge(
			"library_video_codecs",
			"Tdarr video codecs for library by type",
			[]string{"library_name", "library_id", "codec"}, instance,
		),
		pieVideoContainers: newGauge(
			"library_video_containers",
			"Tdarr video containers for library by type",
			[]string{"library_name", "library_id", "container_type"}, instance,
		),
		pieVideoResolutions: newGauge(
			"library_video_resolutions",
			"Tdarr video resolutions for library by type",
			[]string{"library_name", "library_id", "resolution"}, instance,
		),
		pieAudioCodecs: newGauge(
			"library_audio_codecs",
			"Tdarr audio codecs for library by type",
			[]string{"library_name", "library_id", "codec"}, instance,
		),
		pieAudioContainers: newGauge(
			"library_audio_containers",
			"Tdarr audio containers for library by type",
			[]string{"library_name", "library_id", "container_type"}, instance,
		),
		pieAudioResolutions: newGauge(
			"library_audio_resolutions",
			"Tdarr audio resolutions for library by type",
			[]string{"library_name", "library_id", "resolution"}, instance,
		),
		unknownStatusTotal: newCounter(
			"unknown_status_total",
			"Count of pie status values not in the known enum, by job_kind (transcode|healthcheck) and status label. "+
				"A non-zero value indicates Tdarr emitted a status that the exporter does not pre-emit zeros for. "+
				"Use increase(tdarr_unknown_status_total[24h]) > 0 to alert on API drift.",
			[]string{"job_kind", "status"}, instance,
		),
		upMetric: newGauge(
			"up",
			"1 if the last collection cycle succeeded, 0 otherwise (Tdarr API error, response parse error, or partial pie-stats fetch). "+
				"Distinct from prometheus built-in 'up' which indicates exporter process reachability.",
			nil, instance,
		),
		nodeCollector: NewTdarrNodeCollector(runConfig, api, log.Logger),
	}

	// Assemble the collector's own descs once, in Describe order. Describe ranges over
	// this list plus the node collector's descs() — adding a metric means appending here
	// in exactly one place. The order matches the historical hand-written Describe list.
	c.descsList = []typedDesc{
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
		c.pieAudioResolutions,
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
	// Range over the two sources separately rather than append()-ing them into one
	// slice: appending to c.descsList could alias/overwrite its backing array if it
	// ever had spare capacity. Two loops are allocation-free and alias-free.
	for _, d := range c.descsList {
		ch <- d.desc
	}
	for _, d := range c.nodeCollector.metrics.descs() {
		ch <- d.desc
	}
}

func (c *TdarrCollector) httpReqHelper(ctx context.Context, path string, reqPayload any, target any) error {
	c.logger.Debug().Interface("payload", reqPayload).Msg("Requesting statistics data from Tdarr")
	// Marshal it into JSON prior to requesting
	payload, err := json.Marshal(reqPayload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w: %w", ErrParse, err)
	}
	// make request
	httpErr := c.api.DoPostRequest(ctx, path, target, payload)
	if httpErr != nil {
		return fmt.Errorf("request %s: %w: %w", path, ErrUpstream, httpErr)
	}
	c.logger.Debug().Str("urlPath", path).Interface("payload", reqPayload).Interface("response", target).Msg("Stats API Response")
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
func (c *TdarrCollector) getLibStats(ctx context.Context, wg *sync.WaitGroup, inChan <-chan TdarrPieDataRequest, outChan chan<- *TdarrPieStats) {
	defer wg.Done()
	for piePayload := range inChan {
		pieMetric := &TdarrPieStats{}
		c.logger.Debug().Interface("payload", piePayload).Msg("Requesting Lib stats pie data from Tdarr")
		err := c.httpReqHelper(ctx, c.pieStatsPath, piePayload, pieMetric)
		if err != nil {
			c.logger.Error().Interface("payload", piePayload).Err(err).Msg("Failed to get Lib stats pie data")
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
			c.logger.Warn().
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
func (c *TdarrCollector) fetchPies(ctx context.Context, allLibs []TdarrLibraryInfo) []*TdarrPieStats {
	var pieData []*TdarrPieStats

	dataWg := &sync.WaitGroup{}
	inChan := make(chan TdarrPieDataRequest, len(allLibs))
	outChan := make(chan *TdarrPieStats, len(allLibs))
	// start workers
	for i := 0; i < c.maxConcurrency; i++ {
		dataWg.Add(1)
		go c.getLibStats(ctx, dataWg, inChan, outChan)
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

	// Drain results concurrently with the workers. Starting the drain before
	// dataWg.Wait() means correctness no longer depends on outChan being buffered
	// to len(allLibs): even with a smaller (or unbuffered) outChan, workers never
	// block on send because the drain is always consuming. pieData is written only
	// by this goroutine and read only after resultWg.Wait(), so there is no race.
	resultWg := &sync.WaitGroup{}
	resultWg.Add(1)
	go func() {
		defer resultWg.Done()
		for pie := range outChan {
			pieData = append(pieData, pie)
		}
	}()

	// wait for workers to finish producing, then close outChan to stop the drain
	dataWg.Wait()
	close(outChan)

	// wait for results to be collected
	resultWg.Wait()
	return pieData
}

func (c *TdarrCollector) Collect(ch chan<- prometheus.Metric) {
	// Derive a per-scrape context from baseCtx (cancelled on shutdown). The defer
	// releases the context tree when the scrape returns; if baseCtx is cancelled
	// mid-scrape, the in-flight HTTP requests abort.
	ctx, cancel := context.WithCancel(c.baseCtx)
	defer cancel()
	err := c.collect(ctx, ch)
	if err != nil {
		c.logger.Error().Err(err).Msg("Collection cycle failed")
	}
	// Always reset partialFailure flag — must NOT short-circuit via OR or the flag leaks across scrapes.
	partial := c.partialFailure.Swap(false)
	v := 1.0
	if err != nil || partial {
		v = 0.0
	}
	ch <- c.upMetric.mustNewConstMetric(v)
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

func (c *TdarrCollector) collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	// get server metrics
	metricReqBody := getGeneralReqPayload("")
	metric := &TdarrMetric{}
	err := c.httpReqHelper(ctx, c.statsPath, metricReqBody, &metric)
	if err != nil {
		return err
	}

	c.logger.Debug().Int("totalFiles", metric.TotalFileCount).
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
		return fmt.Errorf("parse tdarr score %q: %w: %w", metric.TdarrScore, ErrParse, floatConvertErr)
	}
	healthScore, floatConvertErr = strconv.ParseFloat(metric.HealthCheckScore, 64)
	if floatConvertErr != nil {
		return fmt.Errorf("parse health score %q: %w: %w", metric.HealthCheckScore, ErrParse, floatConvertErr)
	}

	// supports only api versions: v2.24.01+
	c.logger.Debug().Str("path", c.pieStatsPath).Msg("Fetching library pie stats")
	// already have total file count from general stats (`metric.TotalFileCount`)
	// check cache for all libraries data
	// this won't block other reads when checking
	cacheTotals := c.statsCache.GetTotals()
	// Refetch if the cache is empty (first run) or any of the 10 totals changed.
	// Beyond the three top-level counts, the seven per-bucket queue fields
	// (holdQueue/transcodeQueue/… on TdarrMetric) catch state transitions the totals miss
	// (e.g. files queued but not yet transcoded). Their Tdarr JSON source (table0Count–table6Count)
	// and bucket meanings are documented on TdarrMetric in tdarr_models.go. Older Tdarr versions
	// omit those fields; they decode to 0, so 0==0 never triggers a spurious refetch.
	shouldCollect := shouldRefetch(cacheTotals, c.statsCache.GetLibStats() == nil, metric)
	if shouldCollect {
		c.logger.Debug().Msg("Stats totals mismatch - re-fetching library pie stats")
	}
	// if counts are the same use cache
	if !shouldCollect {
		c.logger.Debug().Msg("Using cached library stats - api totals matches cached values")
		pieData = c.statsCache.GetLibStats()
	} else { // fetch new data and update cache
		getLibsPayload := getGeneralReqPayload("library")
		allLibs := []TdarrLibraryInfo{}
		err := c.httpReqHelper(ctx, c.statsPath, getLibsPayload, &allLibs)
		if err != nil {
			return fmt.Errorf("get library details: %w", err)
		}

		pieData = c.fetchPies(ctx, allLibs)
		c.logger.Debug().Msg("All library stats gathered - setting cache")
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
		ch <- c.unknownStatusTotal.mustNewConstMetric(count,
			key.kind, key.status)
	}
	c.unknownStatusMu.Unlock()

	// get all node metrics
	nodeData, err := c.nodeCollector.GetNodeData(ctx)
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
	ch <- c.totalFilesMetric.mustNewConstMetric(float64(metric.TotalFileCount))
	ch <- c.totalTranscodeCount.mustNewConstMetric(float64(metric.TotalTranscodeCount))
	ch <- c.totalHealthCheckCount.mustNewConstMetric(float64(metric.TotalHealthCheckCount))
	ch <- c.sizeDiff.mustNewConstMetric(metric.SizeDiff)
	ch <- c.tdarrScore.mustNewConstMetric(score)
	ch <- c.healthCheckScore.mustNewConstMetric(healthScore)
	ch <- c.avgNumStreams.mustNewConstMetric(metric.AvgNumStreams)
	ch <- c.streamStatsDuration.mustNewConstMetric(float64(metric.StreamStats.Duration.Average), "average")
	ch <- c.streamStatsDuration.mustNewConstMetric(float64(metric.StreamStats.Duration.Highest), "highest")
	ch <- c.streamStatsDuration.mustNewConstMetric(float64(metric.StreamStats.Duration.Total), "total")
	ch <- c.streamStatsBitRate.mustNewConstMetric(float64(metric.StreamStats.BitRate.Average), "average")
	ch <- c.streamStatsBitRate.mustNewConstMetric(float64(metric.StreamStats.BitRate.Highest), "highest")
	ch <- c.streamStatsBitRate.mustNewConstMetric(float64(metric.StreamStats.BitRate.Total), "total")
	ch <- c.streamStatsNumFrames.mustNewConstMetric(float64(metric.StreamStats.NumFrames.Average), "average")
	ch <- c.streamStatsNumFrames.mustNewConstMetric(float64(metric.StreamStats.NumFrames.Highest), "highest")
	ch <- c.streamStatsNumFrames.mustNewConstMetric(float64(metric.StreamStats.NumFrames.Total), "total")
}

// emitPieSlices emits one gauge per slice in a pie-slice list (codecs/containers/resolutions),
// lowercasing the slice name as the final label. Shared by the five video/audio loops.
func emitPieSlices(ch chan<- prometheus.Metric, desc typedDesc, libName, libId string, slices []TdarrPieSlice) {
	for _, pieSlice := range slices {
		ch <- desc.mustNewConstMetric(float64(pieSlice.Value),
			libName, libId, strings.ToLower(pieSlice.Name))
	}
}

// emitPieMetrics emits the per-library pie series (totals, normalized status maps, and the
// video/audio codec/container/resolution slices). Pure: reads pieData, writes to ch.
func (c *TdarrCollector) emitPieMetrics(ch chan<- prometheus.Metric, pieData []*TdarrPieStats) {
	for _, pie := range pieData {
		ch <- c.pieNumFiles.mustNewConstMetric(float64(pie.PieStats.TotalFiles), pie.libraryName, pie.libraryId)
		ch <- c.pieNumTranscodes.mustNewConstMetric(float64(pie.PieStats.TotalTranscodeCount), pie.libraryName, pie.libraryId)
		ch <- c.pieNumHealthChecks.mustNewConstMetric(float64(pie.PieStats.TotalHealthCheckCount), pie.libraryName, pie.libraryId)
		ch <- c.pieSizeDiff.mustNewConstMetric(pie.PieStats.SizeDiff, pie.libraryName, pie.libraryId)
		// Emit transcode statuses from the normalized map (pre-cleaned labels, full enum coverage).
		for status, count := range pie.NormalizedTranscodes {
			ch <- c.pieTranscodes.mustNewConstMetric(float64(count),
				pie.libraryName, pie.libraryId, status)
		}
		// Emit health check statuses from the normalized map (pre-cleaned labels, full enum coverage).
		for status, count := range pie.NormalizedHealthChecks {
			ch <- c.pieHealthChecks.mustNewConstMetric(float64(count),
				pie.libraryName, pie.libraryId, status)
		}
		emitPieSlices(ch, c.pieVideoCodecs, pie.libraryName, pie.libraryId, pie.PieStats.Video.Codecs)
		emitPieSlices(ch, c.pieVideoContainers, pie.libraryName, pie.libraryId, pie.PieStats.Video.Containers)
		emitPieSlices(ch, c.pieVideoResolutions, pie.libraryName, pie.libraryId, pie.PieStats.Video.Resolutions)
		emitPieSlices(ch, c.pieAudioCodecs, pie.libraryName, pie.libraryId, pie.PieStats.Audio.Codecs)
		emitPieSlices(ch, c.pieAudioContainers, pie.libraryName, pie.libraryId, pie.PieStats.Audio.Containers)
		emitPieSlices(ch, c.pieAudioResolutions, pie.libraryName, pie.libraryId, pie.PieStats.Audio.Resolutions)
	}
}

// emitParsedFloat parses raw as a float64 and, on success, emits a node-scoped gauge.
// On parse failure it debug-logs and silently skips the metric (intentional: Tdarr may
// send empty/non-numeric resource strings for nodes that haven't reported yet).
func (c *TdarrCollector) emitParsedFloat(ch chan<- prometheus.Metric, desc typedDesc, raw string, nodeId, nodeName string) {
	if v, floatErr := strconv.ParseFloat(raw, 64); floatErr == nil {
		ch <- desc.mustNewConstMetric(v, nodeId, nodeName)
	} else {
		c.logger.Debug().Str("nodeId", nodeId).Str("nodeName", nodeName).
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
		ch <- m.nodeInfo.mustNewConstMetric(1,
			node.Id, node.Name, node.GpuSelect,
			strconv.Itoa(node.Config.Pid), strconv.Itoa(node.Priority),
			strconv.FormatBool(node.AllowGpuDoCpu),
		)

		// node uptime
		ch <- m.nodeUptime.mustNewConstMetric(
			float64(node.ResourceStats.Process.Uptime), node.Id, node.Name)

		// convert resource stats to float from string; skip on parse failure
		c.emitParsedFloat(ch, m.nodeHeapUsedMb, node.ResourceStats.Process.HeapUsedMb, node.Id, node.Name)
		c.emitParsedFloat(ch, m.nodeHeapTotalMb, node.ResourceStats.Process.HeapTotalMb, node.Id, node.Name)
		c.emitParsedFloat(ch, m.nodeHostCpuPercent, node.ResourceStats.Os.CpuPercent, node.Id, node.Name)
		c.emitParsedFloat(ch, m.nodeHostMemUsedGb, node.ResourceStats.Os.MemUsedGb, node.Id, node.Name)
		c.emitParsedFloat(ch, m.nodeHostMemTotalGb, node.ResourceStats.Os.MemTotalGb, node.Id, node.Name)

		// node state gauges
		pausedVal := 0.0
		if node.Paused {
			pausedVal = 1.0
		}
		ch <- m.nodePaused.mustNewConstMetric(pausedVal, node.Id, node.Name)
		ch <- m.nodeMaxGpuWorkers.mustNewConstMetric(float64(node.MaxGpuWorkers), node.Id, node.Name)
		schedVal := 0.0
		if node.ScheduleEnabled {
			schedVal = 1.0
		}
		ch <- m.nodeScheduleEnabled.mustNewConstMetric(schedVal, node.Id, node.Name)

		// per-type gauges — always emit all four types so zero-value series appear
		emitPerType(ch, m.nodeWorkerLimit, node.Id, node.Name, node.WorkerLimits)
		emitPerType(ch, m.nodeQueueLength, node.Id, node.Name, node.QueueLengths)

		// worker count by type — count from active workers map.
		// Always emit zeros for the four known dims; emit unknown buckets only when non-zero
		// (raw API string preserved as worker_type, "unknown" as compute_type).
		workerCounts := countWorkersByType(node.Workers)
		for _, d := range knownWorkerTypeDims {
			ch <- m.nodeWorkerCount.mustNewConstMetric(
				float64(workerCounts.known[d]), node.Id, node.Name, d.workerType, d.computeType)
		}
		for rawType, count := range workerCounts.unknown {
			if count == 0 {
				continue
			}
			c.logger.Warn().Str("workerType", rawType).Int("count", count).
				Msg("Unknown worker type encountered; bucketing under 'unknown'")
			ch <- m.nodeWorkerCount.mustNewConstMetric(
				float64(count), node.Id, node.Name, rawType, computeTypeUnknown)
		}

		// per-worker metrics
		for _, worker := range node.Workers {
			c.logger.Debug().Interface("worker", worker).Msg("Worker data")

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
			ch <- m.nodeWorkerInfo.mustNewConstMetric(1,
				node.Id, node.Name, worker.Id, wType, cType,
				strconv.FormatBool(worker.FlowWorker),
				worker.Status, worker.File,
				pluginId, pluginPosition,
				strconv.FormatBool(worker.Process.Connected), strconv.FormatBool(worker.Idle),
			)

			// per-worker numeric gauges
			ch <- m.nodeWorkerPercentage.mustNewConstMetric(
				worker.Percentage, node.Id, node.Name, worker.Id)
			ch <- m.nodeWorkerFps.mustNewConstMetric(
				float64(worker.Fps), node.Id, node.Name, worker.Id)
			ch <- m.nodeWorkerOriginalFileSizeGb.mustNewConstMetric(
				worker.OriginalfileSizeGb, node.Id, node.Name, worker.Id)
			ch <- m.nodeWorkerOutputFileSizeGb.mustNewConstMetric(
				worker.OutputFileSizeGb, node.Id, node.Name, worker.Id)
			ch <- m.nodeWorkerEstFileSizeGb.mustNewConstMetric(
				worker.EstSizeGb, node.Id, node.Name, worker.Id)
			ch <- m.nodeWorkerJobStartTimestamp.mustNewConstMetric(
				float64(worker.Job.StartTime), node.Id, node.Name, worker.Id)
			ch <- m.nodeWorkerStartTimestamp.mustNewConstMetric(
				float64(worker.StartTime), node.Id, node.Name, worker.Id)
			ch <- m.nodeWorkerStatusTimestamp.mustNewConstMetric(
				float64(worker.StatusTs), node.Id, node.Name, worker.Id)
			ch <- m.nodeWorkerPid.mustNewConstMetric(
				float64(worker.Process.Pid), node.Id, node.Name, worker.Id)

			// ETA: parse "H:MM:SS" string into seconds; skip on parse failure
			if etaSecs, ok := parseEtaSeconds(worker.Eta); ok {
				ch <- m.nodeWorkerEtaSeconds.mustNewConstMetric(
					float64(etaSecs), node.Id, node.Name, worker.Id)
			} else {
				c.logger.Debug().Str("nodeId", node.Id).Str("workerId", worker.Id).
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
				Obj:        map[string]any{},
			},
		}
	} else {
		return TdarrMetricRequest{
			Data: TdarrDataRequest{
				Collection: "StatisticsJSONDB",
				Mode:       "getById",
				DocId:      "statistics",
				Obj:        map[string]any{},
			},
		}
	}
}
