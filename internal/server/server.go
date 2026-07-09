package server

import (
	"context"
	"encoding/json"
	"fmt"
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

func ServeHttp(wg *sync.WaitGroup, registry *prometheus.Registry, runConfig HttpServerConfig, stopChan chan bool, errChan chan<- error) {
	defer wg.Done()
	mux := http.NewServeMux()
	mux.Handle("GET "+runConfig.PrometheusPath, handlers.MetricsHandler(registry, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError}, runConfig.TdarrInstance))
	mux.Handle("GET /{$}", handlers.IndexHandler())
	mux.Handle("GET /healthz", handlers.HealthzHandler())
	// Fallback for everything else (gin's old NoRoute behavior).
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Warn().
			Str("route", r.URL.Path).
			Msg("Route Not Found")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Route Not Found: Try /metrics"})
	})

	log.Info().
		Str("interface", runConfig.ListenAddress).
		Str("port", runConfig.PrometheusPort).
		Msg("Starting HTTP Server")

	srv := http.Server{
		Addr:    fmt.Sprintf("%s:%s", runConfig.ListenAddress, runConfig.PrometheusPort),
		Handler: handlers.Recovery(handlers.RequestLogger(mux)),
		// Bound header read time so idle half-open connections cannot pin
		// goroutines indefinitely (slowloris; gosec G112).
		ReadHeaderTimeout: 5 * time.Second,
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
