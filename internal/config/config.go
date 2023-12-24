package config

import (
	"flag"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	envTdarrUrl       = "TDARR_URL"
	envSslVerify      = "VERIFY_SSL"
	envPrometheusPort = "PROMETHEUS_PORT"
	envPrometheusPath = "PROMETHEUS_PATH"
	envLogLevel       = "LOG_LEVEL"
)

type Config struct {
	LogLevel           string
	Url                string
	VerifySsl          bool
	PrometheusPort     string
	PrometheusPath     string
	HttpTimeoutSeconds int
	TdarrMetricsPath   string
}

// func setLoggerDefaults() {
// 	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
// }

func setLoggerLevel(logLevel string) {
	// set up global log level for zerolog
	level := strings.ToLower(logLevel)
	switch level {
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case "panic":
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	default:
		log.Fatal().
			Str("log_level", logLevel).
			Msg("Improper log level given!")
	}
}

func newDefaults() Config {
	// get defaults and then replace them with env vars if specified
	defaults := GetDefaults()
	if tdarrUrlEnv := os.Getenv(envTdarrUrl); tdarrUrlEnv != "" {
		defaults.Url = tdarrUrlEnv
	}
	if sslVerifyEnv := os.Getenv(envSslVerify); sslVerifyEnv != "" {
		boolValue, err := strconv.ParseBool(sslVerifyEnv)
		if err != nil {
			log.Fatal().
				Err(err).
				Msg("Invalid value for verify_ssl! Please provide one of true or false.")
		}
		defaults.VerifySsl = boolValue
	}
	if prometheusPortEnv := os.Getenv(envPrometheusPort); prometheusPortEnv != "" {
		defaults.PrometheusPort = prometheusPortEnv
	}
	if prometheusPathEnv := os.Getenv(envPrometheusPath); prometheusPathEnv != "" {
		defaults.PrometheusPath = prometheusPathEnv
	}
	if logLevelEnv := os.Getenv(envLogLevel); logLevelEnv != "" {
		defaults.LogLevel = logLevelEnv
	}
	return defaults
}

func NewConfig() Config {
	defaults := newDefaults()
	log.Info().Str("url", defaults.Url)
	url := flag.String("url", defaults.Url, "valid url for tdarr instance, ex: https://tdarr.somedomain.com")
	sslVerify := flag.Bool("verify_ssl", defaults.VerifySsl, "verify ssl certificates from tdarr")
	promPort := flag.String("prometheus_port", defaults.PrometheusPort, "port for prometheus exporter")
	promPath := flag.String("prometheus_path", defaults.PrometheusPath, "path to use for prometheus exporter")
	logLevel := flag.String("log_level", defaults.LogLevel, "log level to use, see link for possible values: https://pkg.go.dev/github.com/rs/zerolog#Level")
	flag.Parse()
	if *url == "" {
		log.Fatal().
			Msg("A valid url needs to be provided!")
	}

	setLoggerLevel(*logLevel)
	return Config{
		Url:                *url,
		VerifySsl:          *sslVerify,
		PrometheusPort:     *promPort,
		PrometheusPath:     *promPath,
		LogLevel:           *logLevel,
		HttpTimeoutSeconds: defaults.HttpTimeoutSeconds,
		TdarrMetricsPath:   defaults.TdarrMetricsPath,
	}
}

// func init() {
// 	setLoggerDefaults()
// }
