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

	"github.com/rs/zerolog/log"
)

func main() {
	defer os.Exit(0)
	userConfig := config.NewConfig()
	log.Info().Interface("config", userConfig).Msg("Using generated configuration")

	// prometheus set up
	tdarrCollector := collector.NewTdarrCollector(userConfig)
	registry := prometheus.NewRegistry()
	// registering a collector uses JIT? and first scrape will be slower
	registry.MustRegister(tdarrCollector)

	// http server
	stopHttpChan := make(chan bool)
	httpWg := &sync.WaitGroup{}
	httpServerConfig := server.HttpServerConfig{
		TdarrUrl:        userConfig.Url,
		PrometheusPort:  userConfig.PrometheusPort,
		PrometheusPath:  userConfig.PrometheusPath,
		ListenAddress:   "0.0.0.0",
		GracefulTimeout: 30 * time.Second,
	}
	httpWg.Add(1)
	go server.ServeHttp(httpWg, registry, httpServerConfig, stopHttpChan)

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
	<-quitServer
	log.Info().Msg("Received Interrupt - shutting down...")
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
