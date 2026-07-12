package handlers

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler returns the promhttp handler for the registry. The handler's
// own instrumentation counters are labeled with tdarr_instance to match the
// const label on the collector metrics. HandlerFor keeps the raw reg (it
// gathers the real metrics); only the handler-internal counters are wrapped.
// Setting opts.Registry routes promhttp_metric_handler_errors_total through
// instReg too (it is registered only when opts.Registry != nil).
func MetricsHandler(reg *prometheus.Registry, opts promhttp.HandlerOpts, tdarrInstance string) http.Handler {
	instReg := prometheus.WrapRegistererWith(prometheus.Labels{"tdarr_instance": tdarrInstance}, reg)
	opts.Registry = instReg
	return promhttp.InstrumentMetricHandler(instReg, promhttp.HandlerFor(reg, opts))
}
