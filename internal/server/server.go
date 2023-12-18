package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/homeylab/tdarr-exporter/internal/handlers"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

var GRACEFUL_TIMEOUT = 30 * time.Second

type registerFunc func(registry prometheus.Registerer)

func ServeHttp(fn registerFunc, runConfig config.Config) {
	var srv http.Server

	idleConnsClosed := make(chan struct{})
	go func() {
		sigchan := make(chan os.Signal, 1)
		signal.Notify(sigchan, os.Interrupt)
		signal.Notify(sigchan, syscall.SIGTERM)
		sig := <-sigchan
		log.Info().
			Interface("signal", sig).
			Msg("Shutting down due to signal")

		ctx, cancel := context.WithTimeout(context.Background(), GRACEFUL_TIMEOUT)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Fatal().
				Err(err).
				Msg("Server shutdown failed")
		}
		close(idleConnsClosed)
	}()

	registry := prometheus.NewRegistry()
	fn(registry)

	mux := http.NewServeMux()
	mux.Handle(runConfig.PrometheusPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/", handlers.IndexHandler)
	mux.HandleFunc("/healthz", handlers.HealthzHandler)

	listenAddress := "0.0.0.0"

	log.Info().
		Str("interface", listenAddress).
		Str("port", runConfig.PrometheusPort).
		Msg("Starting HTTP Server")
	srv.Addr = fmt.Sprintf("%s:%s", listenAddress, runConfig.PrometheusPort)

	wrappedMux := handlers.RecoveryHandler(mux)
	wrappedMux = handlers.MetricsHandler(runConfig, registry, wrappedMux)
	wrappedMux = handlers.LogHandler(wrappedMux)

	srv.Handler = wrappedMux

	fmt.Println("here")

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal().
			Err(err).
			Msg("Failed to start HTTP Server")
	}
	<-idleConnsClosed
}
