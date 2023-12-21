package collector

import (
	"encoding/json"
	"fmt"

	"github.com/homeylab/tdarr-exporter/internal/client"
	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

var METRIC_PREFIX = "tdarr"

type TdarrCollector struct {
	config           config.Config
	payload          TdarrMetricRequest
	totalFilesMetric *prometheus.Desc
}

func NewTdarrCollector(runConfig config.Config) *TdarrCollector {
	return &TdarrCollector{
		config:  runConfig,
		payload: getMetricRequest(),
		totalFilesMetric: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "total_files"),
			"Tdarr totalFileCount",
			nil,
			prometheus.Labels{"instance": runConfig.Url},
		),
	}
}

func (collector *TdarrCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- collector.totalFilesMetric
}

func (collector *TdarrCollector) Collect(ch chan<- prometheus.Metric) {
	httpClient, err := client.NewClient(collector.config.Url, collector.config.VerifySsl, collector.config.HttpTimeoutSeconds)
	if err != nil {
		log.Fatal().
			Err(err)
	}
	fmt.Println(collector.payload)
	// Marshal it into JSON prior to requesting
	payload, err := json.Marshal(collector.payload)
	if err != nil {
		log.Fatal().
			Err(err)
	}
	fmt.Println(string(payload))
	responseData := &TdarrDataResponse{}
	httpErr := httpClient.DoPostRequest(collector.config.TdarrMetricsPath, responseData, payload)
	log.Info().Interface("test", responseData).Msg("Output")
	if httpErr != nil {
		log.Error().Err(httpErr).Msg("Failed to get data for Tdarr exporter")
		return
	}
	ch <- prometheus.MustNewConstMetric(collector.totalFilesMetric, prometheus.GaugeValue, 12.221)
	// time.Sleep(50 * time.Second)
}

func getMetricRequest() TdarrMetricRequest {
	return TdarrMetricRequest{
		Data: TdarrDataRequest{
			Collection: "StatisticsJSONDB",
			Mode:       "getById",
			DocId:      "statistics",
		},
	}
}
