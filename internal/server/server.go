package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

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

// newMux builds the exporter's HTTP handler: the metrics/index/healthz routes,
// the catch-all 404, wrapped in the Recovery + RequestLogger middleware. Shared
// by ServeHttp and the server tests so the real routing/middleware stack is what
// gets exercised.
func newMux(runConfig HttpServerConfig, registry *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET "+runConfig.PrometheusPath, handlers.MetricsHandler(registry, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError}, runConfig.TdarrInstance))
	// These hardcoded routes are the "reserved" set that config.go rejects for
	// prometheus_path; keep the two in sync.
	mux.Handle("GET /{$}", handlers.IndexHandler(runConfig.PrometheusPath))
	mux.Handle("GET /healthz", handlers.HealthzHandler())
	// Fallback for everything else (gin's old NoRoute behavior).
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Debug, not Warn: this catch-all also matches internet scanners
		// probing arbitrary paths, which is operationally routine, not a
		// warning-worthy condition.
		log.Debug().
			Str("route", r.URL.Path).
			Msg("Route Not Found")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Route Not Found: Try " + runConfig.PrometheusPath})
	})
	return handlers.Recovery(handlers.RequestLogger(mux))
}

func ServeHttp(wg *sync.WaitGroup, registry *prometheus.Registry, runConfig HttpServerConfig, stopChan chan bool, errChan chan<- error) {
	defer wg.Done()

	log.Info().
		Str("interface", runConfig.ListenAddress).
		Str("port", runConfig.PrometheusPort).
		Msg("Starting HTTP Server")

	srv := http.Server{
		Addr:    net.JoinHostPort(runConfig.ListenAddress, runConfig.PrometheusPort),
		Handler: newMux(runConfig, registry),
		// Bound header read time so idle half-open connections cannot pin
		// goroutines indefinitely (slowloris; gosec G112).
		ReadHeaderTimeout: 5 * time.Second,
		// Reap idle keep-alive connections. WriteTimeout stays 0 (unlimited)
		// on purpose: it would cap total response time, and scrape duration is
		// unbounded by design (lazy per-scrape collection against a possibly
		// slow Tdarr instance).
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
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
