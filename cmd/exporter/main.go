package main

import (
	"context"
	"fmt"
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
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/version"

	"github.com/rs/zerolog/log"
)

func main() {
	os.Exit(run())
}

// run holds the real main body and returns the process exit code: 0 for a
// signal-triggered shutdown, 1 when shutdown was caused by an HTTP server
// error. Split out of main so os.Exit does not skip deferred cleanup.
func run() int {
	userConfig := config.NewConfig()
	if userConfig.Version {
		fmt.Println(version.Print("tdarr_exporter"))
		return 0
	}
	log.Debug().Interface("config", userConfig).Msg("Using generated configuration")
	log.Info().Str("version", version.Version).Str("revision", version.Revision).Str("buildDate", version.BuildDate).Str("goVersion", version.GoVersion).Msg("Starting tdarr-exporter")

	// scrapeCtx is the parent context for all collector HTTP requests; cancelling
	// it (on shutdown, below) aborts any scrape in flight instead of letting it
	// run to completion against a Tdarr instance we are disconnecting from.
	scrapeCtx, cancelScrapes := context.WithCancel(context.Background())
	defer cancelScrapes()

	// prometheus set up
	tdarrCollector, err := collector.NewTdarrCollector(scrapeCtx, userConfig)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create Tdarr collector")
		return 1
	}
	registry := prometheus.NewRegistry()
	// registering a collector uses JIT and first scrape will be slower
	registry.MustRegister(tdarrCollector)
	// standard Go runtime + process metrics (go_*, process_*)
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	// build info metric (tdarr_exporter_build_info)
	registry.MustRegister(versioncollector.NewCollector("tdarr_exporter"))

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
	// triggers the same graceful-shutdown path but yields a non-zero exit code.
	exitCode := 0
	select {
	case <-quitServer:
		log.Info().Msg("Received Interrupt - shutting down...")
	case err := <-errHttpChan:
		log.Error().Err(err).Msg("HTTP server error - shutting down...")
		exitCode = 1
	}
	go func() {
		<-quitServer
		log.Fatal().Msg("Killing app on 2nd forced interrupt...")
	}()
	// Abort any in-flight scrape before tearing down the HTTP server.
	cancelScrapes()
	stopHttpChan <- true
	httpWg.Wait()
	log.Info().Msg("Gracefully shutdown tdarr exporter")
	return exitCode
}

func init() {
	gin.SetMode(gin.ReleaseMode)
}
