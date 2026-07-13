package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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
	registry := buildRegistry(userConfig.InstanceName, tdarrCollector)

	// http server
	stopHttpChan := make(chan bool)
	// Buffered with one slot per potential sender (ListenAndServe error,
	// Shutdown error) so the server goroutine never blocks sending, even if
	// both fire after main has stopped selecting on errChan.
	errHttpChan := make(chan error, 2)
	httpWg := &sync.WaitGroup{}
	httpServerConfig := server.HttpServerConfig{
		TdarrInstance:   userConfig.InstanceName,
		PrometheusPort:  userConfig.PrometheusPort,
		PrometheusPath:  userConfig.PrometheusPath,
		ListenAddress:   userConfig.ListenAddress,
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
	// Shut down on either an OS signal or a fatal server error; the latter
	// yields a non-zero exit code.
	exitCode := awaitShutdown(quitServer, errHttpChan)
	go func() {
		sig := <-quitServer
		log.Warn().Str("signal", sig.String()).Msg("Forcing immediate shutdown on signal")
		os.Exit(forcedExitCode(sig))
	}()
	// Abort any in-flight scrape before tearing down the HTTP server.
	cancelScrapes()
	stopHttpChan <- true
	httpWg.Wait()
	log.Info().Msg("Gracefully shutdown tdarr exporter")
	return exitCode
}

// buildRegistry assembles the Prometheus registry: the Tdarr collector, the
// standard Go runtime + process collectors, and the build-info metric registered
// through an instance-labeled registerer so tdarr_exporter_build_info carries
// tdarr_instance like the exporter's own metrics.
func buildRegistry(instanceName string, tdarrCollector prometheus.Collector) *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(tdarrCollector)
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.WrapRegistererWith(
		prometheus.Labels{"tdarr_instance": instanceName},
		registry,
	).MustRegister(versioncollector.NewCollector("tdarr_exporter"))
	return registry
}

// forcedExitCode maps a signal to the conventional 128+signum exit code that
// shells, Docker, and Kubernetes report for a signal-terminated process (SIGINT
// -> 130, SIGTERM -> 143). This keeps a force-quit distinguishable from
// awaitShutdown's exit 1, which means "HTTP server error". The fallback is
// unreachable for the signals registered above on this unix build (all are
// syscall.Signal); it exists only to keep the mapping total.
//
// These codes (129-143) intentionally exceed os.Exit's documented portable
// range [0, 125]: mimicking signal death is the whole point, and on the only
// shipped platforms (linux/amd64, linux/arm64) exit status is 8-bit, so no
// registered signal wraps mod 256 (max 143).
func forcedExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 1
}

// awaitShutdown blocks until either an OS signal or a fatal HTTP server error
// arrives, logs the reason, and returns the process exit code: 0 for a
// signal-triggered shutdown, 1 when a server error triggered it.
func awaitShutdown(quit <-chan os.Signal, errCh <-chan error) int {
	select {
	case <-quit:
		log.Info().Msg("Received Interrupt - shutting down...")
		return 0
	case err := <-errCh:
		log.Error().Err(err).Msg("HTTP server error - shutting down...")
		return 1
	}
}
