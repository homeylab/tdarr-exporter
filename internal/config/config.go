package config

import (
	// "flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"

	// required by koanf
	// provides GNU-style command line flags
	flag "github.com/spf13/pflag"
)

type Config struct {
	LogLevel           string   `koanf:"log_level"`
	Url                string   `koanf:"tdarr_url"`
	UrlParsed          *url.URL `koanf:"-"`
	InstanceName       string   `koanf:"-"`
	ApiKey             string   `koanf:"tdarr_api_key"`
	VerifySsl          bool     `koanf:"verify_ssl"`
	PrometheusPort     string   `koanf:"prometheus_port"`
	PrometheusPath     string   `koanf:"prometheus_path"`
	HttpTimeoutSeconds int      `koanf:"http_timeout_seconds"`
	TdarrMetricsPath   string   `koanf:"tdarr_metrics_path"`
	TdarrNodePath      string   `koanf:"tdarr_node_path"`
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

// also act as validation for provided url
func parseUrl(urlString string) (*url.URL, error) {
	// get hostname from url
	if !strings.HasPrefix(urlString, "http") {
		log.Debug().Str("url", urlString).Msg("No scheme provided, defaulting to https")
		urlString = "https://" + urlString
	}
	url, err := url.Parse(urlString)
	if err != nil {
		log.Error().Str("url", urlString).Err(err).Msg("Invalid url provided - failed to parse!")
		return nil, err
	}
	return url, nil
}

func InputIntake(flags *flag.FlagSet) error {
	flags.StringP("tdarr_url", "u", "", "valid url for tdarr instance, ex: https://tdarr.somedomain.com")
	flags.StringP("tdarr_api_key", "a", "", "api token for tdarr instance if authentication is enabled")
	flags.Bool("verify_ssl", true, "verify ssl certificates from tdarr")
	flags.String("prometheus_port", "9090", "port for prometheus exporter")
	flags.String("prometheus_path", "/metrics", "path to use for prometheus exporter")
	flags.StringP("log_level", "l", "info", "log level to use, see link for possible values: https://pkg.go.dev/github.com/rs/zerolog#Level")
	// exit cleanly if help is requested
	flags.Usage = func() {
		fmt.Println(flags.FlagUsages())
		os.Exit(0)
	}
	err := flags.Parse(os.Args[1:])
	if err != nil {
		return err
	}
	return nil
}

func (c *Config) validate() error {
	// validate provided log level
	err := setLoggerLevel(c.LogLevel)
	if err != nil {
		return err
	}
	// validate url is not empty
	if c.Url == "" {
		return fmt.Errorf("a valid url needs to be provided")
	}
	// validate url is parseable
	c.UrlParsed, err = parseUrl(c.Url)
	if err != nil {
		return err
	}
	// assign instance name for clean prometheus metrics label
	c.InstanceName = c.UrlParsed.Hostname()
	return nil
}

func NewConfig() (*Config, error) {
	k := koanf.New(".")

	// set defaults
	err := k.Load(confmap.Provider(map[string]interface{}{
		"log_level":            "info",
		"tdarr_url":            "",
		"tdarr_api_key":        "",
		"verify_ssl":           true,
		"prometheus_port":      "9090",
		"prometheus_path":      "/metrics",
		"http_timeout_seconds": 15,
		"tdarr_metrics_path":   "/api/v2/cruddb",
		"tdarr_node_path":      "/api/v2/get-nodes",
	}, "."), nil)
	if err != nil {
		return nil, err
	}

	// load any matching env vars with struct tags
	err = k.Load(env.Provider("", ".", func(s string) string {
		return strings.ToLower(s)
	}), nil)
	if err != nil {
		return nil, err
	}

	// load given command line flags
	flags := flag.NewFlagSet("config", flag.ContinueOnError)
	// ContinueOnError will return an error when `Parse()` fails
	err = InputIntake(flags)
	if err != nil {
		return nil, err
	}
	if err = k.Load(posflag.Provider(flags, ".", k), nil); err != nil {
		return nil, err
	}

	newConf := Config{}

	err = k.Unmarshal("", &newConf)
	if err != nil {
		return nil, err
	}

	// init additional fields and validate
	err = newConf.validate()
	if err != nil {
		return nil, err
	}

	return &newConf, nil
}

// func init() {
// 	setLoggerDefaults()
// }
