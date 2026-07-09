package handlers

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const METRIC_NAMESPACE = "tdarr"

// MetricsHandler returns the promhttp handler for the registry. The handler's
// own instrumentation counters are labeled with tdarr_instance to match the
// const label on the collector metrics. HandlerFor keeps the raw reg (it
// gathers the real metrics); only the handler-internal counters are wrapped.
// Setting opts.Registry routes promhttp_metric_handler_errors_total through
// instReg too (it is registered only when opts.Registry != nil).
//
// NOTE: the scrapDuration timing wrapper is preserved verbatim from the gin
// version (DEPRECATED, off-by-one — reports the previous scrape). It is removed
// in v4 (pre-v4 plan B1); do not alter or "fix" it here.
func MetricsHandler(reg *prometheus.Registry, opts promhttp.HandlerOpts, tdarrInstance string) http.Handler {
	// static metrics always present
	// use promAuto to auto register with existing registry
	scrapDuration := promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Namespace:   METRIC_NAMESPACE,
		Name:        "scrape_duration_seconds",
		Help:        "Duration of the last scrape of metrics from exporter.",
		ConstLabels: prometheus.Labels{"tdarr_instance": tdarrInstance},
	})

	instReg := prometheus.WrapRegistererWith(prometheus.Labels{"tdarr_instance": tdarrInstance}, reg)
	opts.Registry = instReg
	h := promhttp.InstrumentMetricHandler(instReg, promhttp.HandlerFor(reg, opts))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// defer keeps the original (broken) semantics: Set runs after ServeHTTP,
		// so it reports the previous scrape. Preserved intentionally until v4-B1.
		defer func() { scrapDuration.Set(time.Since(start).Seconds()) }()
		// promhttp serves back response
		h.ServeHTTP(w, r)
	})
}
