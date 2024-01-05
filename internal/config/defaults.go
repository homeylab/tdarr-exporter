package config

func GetDefaults() Config {
	return Config{
		LogLevel:           "info",
		VerifySsl:          true,
		PrometheusPort:     "9090",
		PrometheusPath:     "/metrics",
		HttpTimeoutSeconds: 15,
		TdarrMetricsPath:   "/api/v2/cruddb",
		TdarrNodePath:      "/api/v2/get-nodes",
	}
}
