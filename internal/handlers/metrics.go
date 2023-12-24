package handlers

import (
	"fmt"
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

		log.Info().
			Str("method", c.Request.Method).
			Str("request_uri", c.Request.RequestURI).
			Str("proto", c.Request.Proto).
			Interface("duration_seconds", duration.Seconds()).
			Msg("Incoming request")
	}
}

func MetricsHandler(reg *prometheus.Registry, opts promhttp.HandlerOpts) gin.HandlerFunc {
	// static metrics always present
	var (
		// use promAuto to auto register with existing registry
		scrapDuration = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: METRIC_NAMESPACE,
			Name:      "scrape_duration_seconds",
			Help:      "Duration of the last scrape of metrics from exporter.",
			// ConstLabels: prometheus.Labels{"url": conf.URL},
		})
		requestCount = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: METRIC_NAMESPACE,
			Name:      "scrape_requests_total",
			Help:      "Total number of HTTP requests made.",
			// ConstLabels: prometheus.Labels{"url": conf.URL},
		}, []string{"code"})
	)

	h := promhttp.HandlerFor(reg, opts)

	return func(c *gin.Context) {
		defer func() {
			start := time.Now()
			scrapDuration.Set(time.Since(start).Seconds())
			requestCount.WithLabelValues(fmt.Sprintf("%d", c.Writer.Status())).Inc()
		}()
		// promhttp serves back response
		h.ServeHTTP(c.Writer, c.Request)
	}
}
