package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/homeylab/tdarr-exporter/internal/handlers"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

type HttpServerConfig struct {
	TdarrInstance   string
	ListenAddress   string
	PrometheusPort  string
	PrometheusPath  string
	GracefulTimeout time.Duration
}

func ServeHttp(wg *sync.WaitGroup, registry *prometheus.Registry, runConfig HttpServerConfig, stopChan chan bool, errChan chan<- error) {
	defer wg.Done()
	router := gin.New()
	// Recovery returns a middleware that recovers from any panics and writes a 500 if there was one.
	router.Use(gin.Recovery())
	router.NoRoute(func(c *gin.Context) {
		log.Warn().
			Str("route", c.Request.URL.Path).
			Msg("Route Not Found")
		c.JSON(404, gin.H{"error": "Route Not Found: Try /metrics"})
	})

	// add middleware
	router.Use(handlers.RequestLogger())
	// add handlers
	router.GET(runConfig.PrometheusPath, handlers.MetricsHandler(registry, promhttp.HandlerOpts{}, runConfig.TdarrInstance))
	router.GET("/", handlers.IndexHandler())
	router.GET("/healthz", handlers.HealthzHandler())

	log.Info().
		Str("interface", runConfig.ListenAddress).
		Str("port", runConfig.PrometheusPort).
		Msg("Starting HTTP Server")

	srv := http.Server{
		Addr:    fmt.Sprintf("%s:%s", runConfig.ListenAddress, runConfig.PrometheusPort),
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			// Propagate to the caller instead of os.Exit so the graceful
			// shutdown path in main can run.
			log.Error().
				Err(err).
				Msg("Failed to start HTTP Server")
			errChan <- err
		}
	}()
	log.Info().Msg("HTTP Server started")

	// stop server
	<-stopChan
	log.Info().Msg("Shutting down HTTP Server")

	ctx, cancel := context.WithTimeout(context.Background(), runConfig.GracefulTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		// Propagate to the caller instead of os.Exit so the WaitGroup still
		// completes and main can finish its shutdown sequence.
		log.Error().
			Err(err).
			Msg("Server shutdown failed")
		errChan <- err
	}
	log.Info().Msg("HTTP Server shutdown gracefully")
}
