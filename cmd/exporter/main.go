package main

import (
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

	"github.com/rs/zerolog/log"
)

func main() {
	userConfig := config.NewConfig()
	fmt.Println(userConfig)
	log.Info().
		Interface("config", userConfig)

	// prometheus set up
	tdarrCollector := collector.NewTdarrCollector(userConfig)
	registry := prometheus.NewRegistry()
	registry.MustRegister(tdarrCollector)

	// http server
	stopHttpChan := make(chan bool)
	httpWg := &sync.WaitGroup{}
	httpServerConfig := server.HttpServerConfig{
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
	os.Exit(0)
}

func init() {
	gin.SetMode(gin.ReleaseMode)
}