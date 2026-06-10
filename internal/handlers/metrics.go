package handlers

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

const METRIC_NAMESPACE = "tdarr"

// Log internal request
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		t := time.Now()
		c.Next()
		duration := time.Since(t)

		log.Debug().
			Str("method", c.Request.Method).
			Str("request_uri", c.Request.RequestURI).
			Str("proto", c.Request.Proto).
			Interface("duration_seconds", duration.Seconds()).
			Msg("Incoming request")
	}
}

func MetricsHandler(reg *prometheus.Registry, opts promhttp.HandlerOpts, tdarrInstance string) gin.HandlerFunc {
	// static metrics always present
	// use promAuto to auto register with existing registry
	scrapDuration := promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Namespace:   METRIC_NAMESPACE,
		Name:        "scrape_duration_seconds",
		Help:        "Duration of the last scrape of metrics from exporter.",
		ConstLabels: prometheus.Labels{"tdarr_instance": tdarrInstance},
	})

	// Wrap the registry so the promhttp handler's own counters carry tdarr_instance,
	// matching the const label on the custom metrics above. HandlerFor keeps the raw
	// reg (it gathers the real metrics); only the handler-internal counters are wrapped.
	// Setting opts.Registry routes promhttp_metric_handler_errors_total through instReg
	// too (it is registered only when opts.Registry != nil).
	instReg := prometheus.WrapRegistererWith(prometheus.Labels{"tdarr_instance": tdarrInstance}, reg)
	opts.Registry = instReg
	h := promhttp.InstrumentMetricHandler(instReg, promhttp.HandlerFor(reg, opts))

	return func(c *gin.Context) {
		start := time.Now()
		defer func() {
			scrapDuration.Set(time.Since(start).Seconds())
		}()
		// promhttp serves back response
		h.ServeHTTP(c.Writer, c.Request)
	}
}
