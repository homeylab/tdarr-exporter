package config

import (
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
	envTdarrUrl           = "TDARR_URL"
	envTdarrApiKey        = "TDARR_API_KEY"
	envSslVerify          = "VERIFY_SSL"
	envPrometheusPort     = "PROMETHEUS_PORT"
	envPrometheusPath     = "PROMETHEUS_PATH"
	envLogLevel           = "LOG_LEVEL"
	envHttpMaxConcurrency = "HTTP_MAX_CONCURRENCY"
	envHttpTimeoutSeconds = "HTTP_TIMEOUT_SECONDS"
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
	TdarrStatsPath     string
	TdarrPieStatsPath  string
	TdarrNodePath      string
	TdarrStatusPath    string
	HttpMaxConcurrency int
}

// parseLogLevel maps a string log level to a zerolog.Level. It returns an error
// on an unknown level and does NOT mutate the global zerolog level (callers
// apply the level themselves).
func parseLogLevel(logLevel string) (zerolog.Level, error) {
	switch strings.ToLower(logLevel) {
	case "trace":
		return zerolog.TraceLevel, nil
	case "debug":
		return zerolog.DebugLevel, nil
	case "info":
		return zerolog.InfoLevel, nil
	case "warn":
		return zerolog.WarnLevel, nil
	case "error":
		return zerolog.ErrorLevel, nil
	case "fatal":
		return zerolog.FatalLevel, nil
	case "panic":
		return zerolog.PanicLevel, nil
	default:
		return zerolog.NoLevel, fmt.Errorf("improper log level given: %q", logLevel)
	}
}

func getDefaults() Config {
	return Config{
		LogLevel:           "info",
		ApiKey:             "",
		VerifySsl:          true,
		PrometheusPort:     "9090",
		PrometheusPath:     "/metrics",
		HttpTimeoutSeconds: 15,
		// path fields are intentionally not overridable via env/flag.
		TdarrStatsPath:     "/api/v2/cruddb",
		TdarrNodePath:      "/api/v2/get-nodes",
		TdarrPieStatsPath:  "/api/v2/stats/get-pies",
		TdarrStatusPath:    "/api/v2/status",
		HttpMaxConcurrency: 3,
	}
}

// applyEnvDefaults overlays environment variables on top of getDefaults using
// the injected getenv. precedence: defaults -> env (flags are layered later).
func applyEnvDefaults(getenv func(string) string) (Config, error) {
	defaults := getDefaults()
	if tdarrUrlEnv := getenv(envTdarrUrl); tdarrUrlEnv != "" {
		defaults.url = tdarrUrlEnv
	}
	if tdarrApiKeyEnv := getenv(envTdarrApiKey); tdarrApiKeyEnv != "" {
		defaults.ApiKey = tdarrApiKeyEnv
	}
	if sslVerifyEnv := getenv(envSslVerify); sslVerifyEnv != "" {
		boolValue, err := strconv.ParseBool(sslVerifyEnv)
		if err != nil {
			return Config{}, fmt.Errorf("invalid value for verify_ssl, please provide one of true or false: %w", err)
		}
		defaults.VerifySsl = boolValue
	}
	if prometheusPortEnv := getenv(envPrometheusPort); prometheusPortEnv != "" {
		defaults.PrometheusPort = prometheusPortEnv
	}
	if prometheusPathEnv := getenv(envPrometheusPath); prometheusPathEnv != "" {
		defaults.PrometheusPath = prometheusPathEnv
	}
	if logLevelEnv := getenv(envLogLevel); logLevelEnv != "" {
		defaults.LogLevel = logLevelEnv
	}
	if httpMaxConcurrencyEnv := getenv(envHttpMaxConcurrency); httpMaxConcurrencyEnv != "" {
		intValue, err := strconv.Atoi(httpMaxConcurrencyEnv)
		if err != nil {
			return Config{}, fmt.Errorf("invalid value for http_max_concurrency, please provide a valid integer: %w", err)
		}
		defaults.HttpMaxConcurrency = intValue
	}
	if httpTimeoutEnv := getenv(envHttpTimeoutSeconds); httpTimeoutEnv != "" {
		intValue, err := strconv.Atoi(httpTimeoutEnv)
		if err != nil {
			return Config{}, fmt.Errorf("invalid value for http_timeout_seconds, please provide a valid integer: %w", err)
		}
		defaults.HttpTimeoutSeconds = intValue
	}
	return defaults, nil
}

// parseUrl validates and parses the provided url. A missing scheme defaults to
// https. It returns an error instead of exiting on parse failure.
func parseUrl(urlString string) (*url.URL, error) {
	// Detect a scheme by the "://" separator, not a "http" prefix: HasPrefix("http")
	// both misfires on scheme-less hosts that happen to start with "http"
	// (e.g. "http-server.lan") and mangles non-http schemes.
	if !strings.Contains(urlString, "://") {
		log.Warn().Str("url", urlString).Msg("No URL scheme provided, defaulting to https")
		urlString = "https://" + urlString
	}
	parsed, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("invalid url provided - failed to parse %q: %w", urlString, err)
	}
	return parsed, nil
}

// parseConfig is the pure, testable core of configuration loading. It applies
// precedence defaults -> env -> flags using the injected flag set, args, and
// getenv, and returns validation errors instead of exiting. It does NOT mutate
// any global state (including the zerolog level).
func parseConfig(fs *flag.FlagSet, args []string, getenv func(string) string) (Config, error) {
	defaults, err := applyEnvDefaults(getenv)
	if err != nil {
		return Config{}, err
	}

	url := fs.String("url", defaults.url, "valid url for tdarr instance, ex: https://tdarr.somedomain.com")
	apiKeyAuth := fs.String("api_key", defaults.ApiKey, "api token for tdarr instance if authentication is enabled")
	sslVerify := fs.Bool("verify_ssl", defaults.VerifySsl, "verify ssl certificates from tdarr")
	promPort := fs.String("prometheus_port", defaults.PrometheusPort, "port for prometheus exporter")
	promPath := fs.String("prometheus_path", defaults.PrometheusPath, "path to use for prometheus exporter")
	logLevel := fs.String("log_level", defaults.LogLevel, "log level to use, see link for possible values: https://pkg.go.dev/github.com/rs/zerolog#Level")
	httpMaxConcurrency := fs.Int("http_max_concurrency", defaults.HttpMaxConcurrency, "maximum number of concurrent http requests to make when requesting per Library stats")
	httpTimeoutSeconds := fs.Int("http_timeout_seconds", defaults.HttpTimeoutSeconds, "timeout in seconds for http requests to the tdarr instance")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if *url == "" {
		return Config{}, fmt.Errorf("a valid url needs to be provided")
	}
	if *httpMaxConcurrency <= 0 {
		return Config{}, fmt.Errorf("http_max_concurrency must be at least 1 (single connection)")
	}
	if *httpTimeoutSeconds <= 0 {
		return Config{}, fmt.Errorf("http_timeout_seconds must be at least 1")
	}

	// validate the log level here (without mutating global state).
	if _, err := parseLogLevel(*logLevel); err != nil {
		return Config{}, err
	}

	urlParsed, err := parseUrl(*url)
	if err != nil {
		return Config{}, err
	}

	return Config{
		url:                *url,
		UrlParsed:          urlParsed,
		InstanceName:       urlParsed.Hostname(),
		ApiKey:             *apiKeyAuth,
		VerifySsl:          *sslVerify,
		PrometheusPort:     *promPort,
		PrometheusPath:     *promPath,
		LogLevel:           *logLevel,
		HttpTimeoutSeconds: *httpTimeoutSeconds,
		// path fields are intentionally not overridable.
		TdarrStatsPath:     defaults.TdarrStatsPath,
		TdarrNodePath:      defaults.TdarrNodePath,
		TdarrPieStatsPath:  defaults.TdarrPieStatsPath,
		TdarrStatusPath:    defaults.TdarrStatusPath,
		HttpMaxConcurrency: *httpMaxConcurrency,
	}, nil
}

// NewConfig is the production entrypoint (composition root). It wires os.Args
// and os.Getenv into the testable parseConfig core, then applies the global
// side effects (log level mutation, fatal on error, startup logging) that must
// not live in the testable core.
func NewConfig() Config {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	cfg, err := parseConfig(fs, os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// parseConfig already validated the log level; apply it globally here.
	level, _ := parseLogLevel(cfg.LogLevel)
	zerolog.SetGlobalLevel(level)

	log.Info().Str("url", cfg.UrlParsed.String()).Msg("Using provided full url for tdarr instance")
	return cfg
}
