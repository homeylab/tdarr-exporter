package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	envTdarrUrl       = "TDARR_URL"
	envTdarrApiKey    = "TDARR_API_KEY"
	envSslVerify      = "VERIFY_SSL"
	envPrometheusPort = "PROMETHEUS_PORT"
	envPrometheusPath = "PROMETHEUS_PATH"
	envLogLevel       = "LOG_LEVEL"
)

type Config struct {
	LogLevel           string
	url                string
	UrlParsed          *url.URL
	InstanceName       string
	ApiKey             string
	VerifySsl          bool
	PrometheusPort     string
	PrometheusPath     string
	HttpTimeoutSeconds int
	TdarrMetricsPath   string
	TdarrNodePath      string
}

// func setLoggerDefaults() {
// 	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
// }

func setLoggerLevel(logLevel string) error {
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
		// log.Fatal().
		// 	Str("log_level", logLevel).
		// 	Msg("Improper log level given!")
		return fmt.Errorf("improper log level given: %s", logLevel)
	}
	return nil
}

func getDefaults() Config {
	return Config{
		LogLevel:           "info",
		ApiKey:             "",
		VerifySsl:          true,
		PrometheusPort:     "9090",
		PrometheusPath:     "/metrics",
		HttpTimeoutSeconds: 15,
		TdarrMetricsPath:   "/api/v2/cruddb",
		TdarrNodePath:      "/api/v2/get-nodes",
	}
}

func newDefaults() (Config, error) {
	// get defaults and then replace them with env vars if specified
	defaults := getDefaults()
	if tdarrUrlEnv := os.Getenv(envTdarrUrl); tdarrUrlEnv != "" {
		defaults.url = tdarrUrlEnv
	}
	if tdarrApiKeyEnv := os.Getenv(envTdarrApiKey); tdarrApiKeyEnv != "" {
		defaults.ApiKey = tdarrApiKeyEnv
	}
	if sslVerifyEnv := os.Getenv(envSslVerify); sslVerifyEnv != "" {
		boolValue, err := strconv.ParseBool(sslVerifyEnv)
		if err != nil {
			log.Error().
				Err(err).
				Msg("Invalid value for verify_ssl! Please provide one of true or false.")
			return Config{}, fmt.Errorf("invalid value for verify_ssl, should be true or false: %w", err)
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
	return defaults, nil
}

// also act as validation for provided url
func parseUrl(urlString string) *url.URL {
	// get hostname from url
	if !strings.HasPrefix(urlString, "http") {
		log.Warn().Str("url", urlString).Msg("No scheme provided, defaulting to https")
		urlString = "https://" + urlString
	}
	url, err := url.Parse(urlString)
	if err != nil {
		log.Fatal().Str("url", urlString).Err(err).Msg("Invalid url provided - failed to parse!")
	}
	return url
}

func NewConfig() (Config, error) {
	defaults, defaultsErr := newDefaults()
	if defaultsErr != nil {
		return Config{}, defaultsErr
	}
	url := flag.String("url", defaults.url, "valid url for tdarr instance, ex: https://tdarr.somedomain.com")
	apiKeyAuth := flag.String("api_key", defaults.ApiKey, "api token for tdarr instance if authentication is enabled")
	sslVerify := flag.Bool("verify_ssl", defaults.VerifySsl, "verify ssl certificates from tdarr")
	promPort := flag.String("prometheus_port", defaults.PrometheusPort, "port for prometheus exporter")
	promPath := flag.String("prometheus_path", defaults.PrometheusPath, "path to use for prometheus exporter")
	logLevel := flag.String("log_level", defaults.LogLevel, "log level to use, see link for possible values: https://pkg.go.dev/github.com/rs/zerolog#Level")
	flag.Parse()
	if *url == "" {
		return Config{}, errors.New("a valid url needs to be provided")
	}
	logErr := setLoggerLevel(*logLevel)
	if logErr != nil {
		return Config{}, logErr
	}

	urlParsed := parseUrl(*url)
	log.Info().Str("url", urlParsed.String()).Msg("Using provided full url for tdarr instance")

	return Config{
		url:                *url,
		UrlParsed:          urlParsed,
		InstanceName:       urlParsed.Hostname(),
		ApiKey:             *apiKeyAuth,
		VerifySsl:          *sslVerify,
		PrometheusPort:     *promPort,
		PrometheusPath:     *promPath,
		LogLevel:           *logLevel,
		HttpTimeoutSeconds: defaults.HttpTimeoutSeconds,
		TdarrMetricsPath:   defaults.TdarrMetricsPath,
		TdarrNodePath:      defaults.TdarrNodePath,
	}, nil
}

// func init() {
// 	setLoggerDefaults()
// }
