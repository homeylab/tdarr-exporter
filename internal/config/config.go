package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// errFlagParse marks errors from flag parsing itself (unknown flag, bad value
// syntax) as opposed to semantic validation errors. NewConfig maps it to exit
// code 2, matching the stdlib flag package's ExitOnError behavior.
var errFlagParse = errors.New("invalid command line arguments")

const (
	envTdarrUrl           = "TDARR_URL"
	envTdarrApiKey        = "TDARR_API_KEY"
	envSslVerify          = "VERIFY_SSL"
	envPrometheusPort     = "PROMETHEUS_PORT"
	envPrometheusPath     = "PROMETHEUS_PATH"
	envLogLevel           = "LOG_LEVEL"
	envHttpMaxConcurrency = "HTTP_MAX_CONCURRENCY"
	envHttpTimeoutSeconds = "HTTP_TIMEOUT_SECONDS"
	envListenAddress      = "LISTEN_ADDRESS"
	envInstanceName       = "INSTANCE_NAME"
)

type Config struct {
	Version            bool
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
	ListenAddress      string
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
		ListenAddress:      "0.0.0.0",
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
	if v := getenv(envListenAddress); v != "" {
		defaults.ListenAddress = v
	}
	if v := getenv(envInstanceName); v != "" {
		defaults.InstanceName = v
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
	// url.Parse accepts host-less URLs like "https://"; without a host every
	// outbound request would fail at scrape time (and InstanceName would
	// default to empty), so fail startup instead.
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid url provided - missing host in %q", urlString)
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
	httpTimeoutSeconds := fs.Int("http_timeout_seconds", defaults.HttpTimeoutSeconds, "total time budget in seconds for an http request to the tdarr instance, including transport-level retries and backoff (a low value can silently truncate retries)")
	versionFlag := fs.Bool("version", false, "print version information and exit")
	listenAddress := fs.String("listen_address", defaults.ListenAddress, "network interface address for the exporter's http server to listen on, ex: 127.0.0.1 or ::")
	instanceName := fs.String("instance_name", defaults.InstanceName, "set to customize the tdarr_instance label (defaults to the url hostname); helpful when running multiple exporters and/or multiple tdarr instances on one host")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Config{}, err
		}
		// Tag flag syntax errors so NewConfig can exit 2 (the stdlib/GNU
		// usage-error convention) instead of 1 like semantic config errors.
		return Config{}, fmt.Errorf("%w: %w", errFlagParse, err)
	}

	if *versionFlag {
		// Short-circuit: -version must work without any other config present.
		return Config{Version: true}, nil
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
	// ParseUint with bitSize 16 rejects out-of-range and signed ports for free;
	// port 0 is a valid uint16 but means "pick a random port", so reject it
	// explicitly. Fails fast here rather than later at ListenAndServe.
	if port, err := strconv.ParseUint(*promPort, 10, 16); err != nil || port == 0 {
		return Config{}, fmt.Errorf("prometheus_port must be an integer between 1 and 65535, got %q", *promPort)
	}
	// PrometheusPath is spliced into an http.ServeMux pattern ("GET "+path) at
	// registration (internal/server/server.go, which also hardcodes "/{$}" for
	// the index and "/healthz"). ServeMux panics at registration on several
	// malformed patterns, so validate here to fail cleanly at startup instead
	// of crashing the ServeHttp goroutine. Keep the reserved list below in sync
	// with those hardcoded routes.
	if !strings.HasPrefix(*promPath, "/") {
		return Config{}, fmt.Errorf("prometheus_path must start with '/', got %q", *promPath)
	}
	// '{' and '}' are ServeMux wildcard metacharacters: they make registration
	// panic (e.g. "/metrics/{") or silently register a wildcard route (e.g.
	// "/{id}"). Neither is valid in a metrics path.
	if strings.ContainsAny(*promPath, "{}") {
		return Config{}, fmt.Errorf("prometheus_path %q must not contain '{' or '}'", *promPath)
	}
	// Whitespace splits the "GET "+path pattern string, so registration panics.
	if strings.ContainsFunc(*promPath, unicode.IsSpace) {
		return Config{}, fmt.Errorf("prometheus_path %q must not contain whitespace", *promPath)
	}
	// ServeMux also panics on an unclean pattern path (".", ".." or "//"
	// segments, e.g. "/metrics//foo" or "/foo/.."). Require a canonical path;
	// path.Clean also strips a trailing slash, so "/metrics/" is rejected in
	// favor of "/metrics".
	if *promPath != path.Clean(*promPath) {
		return Config{}, fmt.Errorf("prometheus_path %q must be a clean path (no '.', '..', '//', or trailing slash)", *promPath)
	}
	// Each built-in route (index at "/", "/healthz") already claims its path; a
	// PrometheusPath equal to one collides and panics ServeMux at registration.
	if *promPath == "/" || *promPath == "/healthz" {
		return Config{}, fmt.Errorf("prometheus_path %q conflicts with a reserved exporter route", *promPath)
	}

	// validate the log level here (without mutating global state).
	if _, err := parseLogLevel(*logLevel); err != nil {
		return Config{}, err
	}

	urlParsed, err := parseUrl(*url)
	if err != nil {
		return Config{}, err
	}

	name := *instanceName
	if name == "" {
		name = urlParsed.Hostname()
	}

	return Config{
		url:                *url,
		UrlParsed:          urlParsed,
		InstanceName:       name,
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
		ListenAddress:      *listenAddress,
	}, nil
}

// NewConfig is the production entrypoint (composition root). It wires os.Args
// and os.Getenv into the testable parseConfig core, then applies the global
// side effects (log level mutation, fatal on error, startup logging) that must
// not live in the testable core.
func NewConfig() Config {
	// ContinueOnError (not ExitOnError) so parse outcomes surface here as
	// errors and each maps to the right exit path below: help -> 0, flag
	// syntax error -> 2 (stdlib convention), semantic config error -> 1.
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	cfg, err := parseConfig(fs, os.Args[1:], os.Getenv)
	if err != nil {
		// -h/-help: usage was already printed by fs.Parse; that's a successful
		// outcome, not a config failure.
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		// Flag syntax errors: fs.Parse already printed the offending flag and
		// usage to stderr, so don't log the same thing again — just exit 2
		// like the stdlib's ExitOnError would.
		if errors.Is(err, errFlagParse) {
			os.Exit(2)
		}
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	if cfg.Version {
		return cfg
	}

	// parseConfig already validated the log level; apply it globally here.
	level, _ := parseLogLevel(cfg.LogLevel)
	zerolog.SetGlobalLevel(level)

	log.Info().Str("url", cfg.UrlParsed.String()).Msg("Using provided full url for tdarr instance")
	return cfg
}
