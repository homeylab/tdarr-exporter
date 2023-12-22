package collector

import (
	"encoding/json"
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
	pieNumFiles           *prometheus.Desc
	pieNumTranscodes      *prometheus.Desc
	pieNumHealthChecks    *prometheus.Desc
	pieSizeDiff           *prometheus.Desc
	pieTranscodes         *prometheus.Desc
	pieHealthChecks       *prometheus.Desc
	pieVideoCodecs        *prometheus.Desc
	pieVideoContainers    *prometheus.Desc
	pieVideoResolutions   *prometheus.Desc
	errorMetric           *prometheus.Desc // Error Description for use with InvalidMetric
}

func NewTdarrCollector(runConfig config.Config) *TdarrCollector {
	return &TdarrCollector{
		config:  runConfig,
		payload: getRequestPayload(),
		totalFilesMetric: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "files_total"),
			"Tdarr total file count - includes files in ignore lists within each library",
			nil,
			prometheus.Labels{"instance": runConfig.Url},
		),
		totalTranscodeCount: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "transcodes_total"),
			"Tdarr total transcode count for all libraries",
			nil,
			prometheus.Labels{"instance": runConfig.Url},
		),
		totalHealthCheckCount: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "health_checks_total"),
			"Tdarr total health check count for all libraries",
			nil,
			prometheus.Labels{"instance": runConfig.Url},
		),
		sizeDiff: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "size_diff_gb"),
			"Tdarr size difference (+/-) in GB",
			nil,
			prometheus.Labels{"instance": runConfig.Url},
		),
		tdarrScore: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "score_pct"),
			"Tdarr score percentage - how much of your library is being handled by tdarr",
			nil,
			prometheus.Labels{"instance": runConfig.Url},
		),
		healthCheckScore: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "health_check_score_pct"),
			"Tdarr health check score percentage - how much of your library is has been health checked by tdarr",
			nil,
			prometheus.Labels{"instance": runConfig.Url},
		),
		pieNumFiles: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_files_total"),
			"Tdarr total files in library",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"instance": runConfig.Url},
		),
		pieNumTranscodes: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_transcodes_total"),
			"Tdarr total transcodes for library by status",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"instance": runConfig.Url},
		),
		pieNumHealthChecks: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_health_checks_total"),
			"Tdarr total health checks for library by status",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"instance": runConfig.Url},
		),
		pieSizeDiff: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_size_diff_gb"),
			"Tdarr size difference (+/-) in GB for library",
			[]string{"library_name", "library_id"},
			prometheus.Labels{"instance": runConfig.Url},
		),
		pieTranscodes: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_transcodes"),
			"Tdarr transcodes for library by status",
			[]string{"library_name", "library_id", "status"},
			prometheus.Labels{"instance": runConfig.Url},
		),
		pieHealthChecks: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_health_checks"),
			"Tdarr health checks for library by status",
			[]string{"library_name", "library_id", "status"},
			prometheus.Labels{"instance": runConfig.Url},
		),
		pieVideoCodecs: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_video_codecs"),
			"Tdarr health checks for library by status",
			[]string{"library_name", "library_id", "codec"},
			prometheus.Labels{"instance": runConfig.Url},
		),
		pieVideoContainers: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_video_containers"),
			"Tdarr health checks for library by status",
			[]string{"library_name", "library_id", "container"},
			prometheus.Labels{"instance": runConfig.Url},
		),
		pieVideoResolutions: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "library_video_resolutions"),
			"Tdarr health checks for library by status",
			[]string{"library_name", "library_id", "resolution"},
			prometheus.Labels{"instance": runConfig.Url},
		),
		errorMetric: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "collector_error"),
			"Error while collecting metrics",
			nil,
			prometheus.Labels{"instance": runConfig.Url},
		),
	}
}

func (c *TdarrCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.totalFilesMetric
}

func (c *TdarrCollector) Collect(ch chan<- prometheus.Metric) {
	httpClient, err := client.NewRequestClient(c.config.Url, c.config.VerifySsl)
	if err != nil {
		log.Error().
			Err(err).Msg("Failed to create http request client for Tdarr, ensure proper URL is provided")
		ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
	}
	log.Debug().Interface("payload", c.payload).Msg("Requesting statistics data from Tdarr")
	// Marshal it into JSON prior to requesting
	payload, err := json.Marshal(c.payload)
	if err != nil {
		log.Error().Err(err).Interface("payload", c.payload).
			Msg("Failed to marshal payload for statistics request")
		ch <- prometheus.NewInvalidMetric(c.errorMetric, err)
	}
	// get the post request payload to use
	metric := &TdarrMetric{}
	// make request
	httpErr := httpClient.DoPostRequest(c.config.TdarrMetricsPath, metric, payload)
	if httpErr != nil {
		log.Error().Err(httpErr).Msg("Failed to get data for Tdarr exporter")
		ch <- prometheus.NewInvalidMetric(c.errorMetric, httpErr)
		return
	}
	log.Info().Interface("response", metric).Msg("Output")
	var (
		pieData         []TdarrPie
		score           float64
		healthScore     float64
		floatConvertErr error
		// pieMetricsErr   error
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
		pieData = append(pieData, TdarrPie{
			LibraryName:              pie[0].(string),
			LibraryId:                pie[1].(string),
			NumFiles:                 int(pie[2].(float64)),
			NumTranscodes:            int(pie[3].(float64)),
			SpaceSavedGB:             pie[4].(float64),
			NumHealthChecks:          int(pie[5].(float64)),
			TdarrTranscodePie:        transcodePie,
			TdarrHealthPie:           healthPie,
			TdarrVideoCodecsPie:      videoCodecsPie,
			TdarrVideoContainersPie:  videoContainersPie,
			TdarrVideoResolutionsPie: videoResolutionsPie,
		})
	}
	ch <- prometheus.MustNewConstMetric(c.totalFilesMetric, prometheus.GaugeValue, float64(metric.TotalFileCount))
	ch <- prometheus.MustNewConstMetric(c.totalTranscodeCount, prometheus.GaugeValue, float64(metric.TotalTranscodeCount))
	ch <- prometheus.MustNewConstMetric(c.totalHealthCheckCount, prometheus.GaugeValue, float64(metric.TotalHealthCheckCount))
	ch <- prometheus.MustNewConstMetric(c.sizeDiff, prometheus.GaugeValue, metric.SizeDiff)
	ch <- prometheus.MustNewConstMetric(c.tdarrScore, prometheus.GaugeValue, score)
	ch <- prometheus.MustNewConstMetric(c.healthCheckScore, prometheus.GaugeValue, healthScore)
	for _, pie := range pieData {
		libraryId := pie.LibraryId
		if strings.ToLower(libraryId) == "all" {
			libraryId = "all_libraries"
		}
		ch <- prometheus.MustNewConstMetric(c.pieNumFiles, prometheus.GaugeValue, float64(pie.NumFiles), pie.LibraryName, libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieNumTranscodes, prometheus.GaugeValue, float64(pie.NumTranscodes), pie.LibraryName, libraryId)
		ch <- prometheus.MustNewConstMetric(c.pieNumHealthChecks, prometheus.GaugeValue, float64(pie.NumHealthChecks), pie.LibraryName, libraryId)
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
		},
	}
}
