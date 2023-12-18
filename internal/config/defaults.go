package config

// import (
// 	"github.com/homeylab/tdarr-exporter/internal/config"
// )

const (
	defaultListenPort         = "9090"
	defaultPath               = "/metrics"
	defaultSslVerify          = true
	defaultLogLevel           = "info"
	defaultHttpTimeoutSeconds = 10
)

func GetDefaults() Config {
	return Config{
		LogLevel:           defaultLogLevel,
		VerifySsl:          defaultSslVerify,
		PrometheusPort:     defaultListenPort,
		PrometheusPath:     defaultPath,
		HttpTimeoutSeconds: defaultHttpTimeoutSeconds,
	}
}
