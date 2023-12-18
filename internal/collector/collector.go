package collector

import (
	"time"

	"github.com/homeylab/tdarr-exporter/internal/client"
	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

var METRIC_PREFIX = "tdarr"

const (
	metricsPath        = "/api/v2/cruddb"
	dataCollectionName = "StatisticsJSONDB"
	dataMode           = "getById"
	dataDocId          = "statistics"
)

type MetricRequest struct {
	Data DataRequest `json:"data"`
}

type DataRequest struct {
	Collection string `json:"collection"`
	Mode       string `json:"mode"`
	DocId      string `json:"docID"`
}

type TdarrCollector struct {
	config           config.Config
	payload          MetricRequest
	totalFilesMetric *prometheus.Desc
}

func NewTdarrCollector(runConfig config.Config) *TdarrCollector {
	return &TdarrCollector{
		config:  runConfig,
		payload: getMetricRequest(),
		totalFilesMetric: prometheus.NewDesc(
			prometheus.BuildFQName(METRIC_PREFIX, "", "total_files"),
			"Tdarr stat totalFileCount",
			[]string{"url"},
			nil,
		),
	}
}

func (t *TdarrCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- t.totalFilesMetric
}

func (t *TdarrCollector) Collect(ch chan<- prometheus.Metric) {
	_, err := client.NewClient(t.config.Url, t.config.VerifySsl, t.config.HttpTimeoutSeconds)
	if err != nil {
		log.Fatal().
			Err(err)
	}
	time.Sleep(50 * time.Second)
}

func getMetricRequest() MetricRequest {
	return MetricRequest{
		Data: DataRequest{
			Collection: dataCollectionName,
			Mode:       dataMode,
			DocId:      dataDocId,
		},
	}
}
