package main

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/homeylab/tdarr-exporter/internal/collector"
	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/homeylab/tdarr-exporter/internal/server"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/rs/zerolog/log"
)

// Build metadata injected by the linker via -X flags.
var (
	version   string
	buildTime string
	revision  string
)

func main() {
	defer os.Exit(0)
	userConfig := config.NewConfig()
	log.Debug().Interface("config", userConfig).Msg("Using generated configuration")
	log.Info().Str("version", version).Str("buildTime", buildTime).Str("revision", revision).Msg("Starting tdarr-exporter")

	// prometheus set up
	tdarrCollector, err := collector.NewTdarrCollector(userConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create Tdarr collector")
	}
	registry := prometheus.NewRegistry()
	// registering a collector uses JIT and first scrape will be slower
	registry.MustRegister(tdarrCollector)
	// standard Go runtime + process metrics (go_*, process_*)
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	// http server
	stopHttpChan := make(chan bool)
	// Buffered so the server goroutine never blocks sending an error (e.g. a
	// late Shutdown error after main has stopped selecting on errChan).
	errHttpChan := make(chan error, 1)
	httpWg := &sync.WaitGroup{}
	httpServerConfig := server.HttpServerConfig{
		TdarrInstance:   userConfig.InstanceName,
		PrometheusPort:  userConfig.PrometheusPort,
		PrometheusPath:  userConfig.PrometheusPath,
		ListenAddress:   "0.0.0.0",
		GracefulTimeout: 30 * time.Second,
	}
	httpWg.Add(1)
	go server.ServeHttp(httpWg, registry, httpServerConfig, stopHttpChan, errHttpChan)

	// graceful shutdown
	quitServer := make(chan os.Signal, 1)
	signal.Notify(
		quitServer,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGTERM,
	)
	// Shut down on either an OS signal or a fatal server error. A server error
	// triggers the same graceful-shutdown path instead of hanging silently.
	select {
	case <-quitServer:
		log.Info().Msg("Received Interrupt - shutting down...")
	case err := <-errHttpChan:
		log.Error().Err(err).Msg("HTTP server error - shutting down...")
	}
	go func() {
		<-quitServer
		log.Fatal().Msg("Killing app on 2nd forced interrupt...")
	}()
	stopHttpChan <- true
	httpWg.Wait()
	log.Info().Msg("Gracefully shutdown tdarr exporter")
}

func init() {
	gin.SetMode(gin.ReleaseMode)
}
