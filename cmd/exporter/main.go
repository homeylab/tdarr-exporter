package main

import (
	"fmt"

	"github.com/homeylab/tdarr-exporter/internal/collector"
	"github.com/homeylab/tdarr-exporter/internal/config"
	"github.com/homeylab/tdarr-exporter/internal/server"

	"github.com/rs/zerolog/log"

	"github.com/prometheus/client_golang/prometheus"
)

func main() {
	newConfig := config.NewConfig()
	fmt.Println(newConfig)
	log.Info().
		Interface("config", newConfig)
	server.ServeHttp(func(r prometheus.Registerer) {
		r.MustRegister(
			collector.NewTdarrCollector(newConfig),
		)
	}, newConfig)
}
