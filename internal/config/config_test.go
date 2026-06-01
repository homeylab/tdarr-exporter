package config

import (
	"flag"
	"testing"

	"github.com/rs/zerolog"
)

// envFunc returns a getenv-compatible function backed by the given map.
func envFunc(env map[string]string) func(string) string {
	return func(key string) string {
		return env[key]
	}
}

func newFS() *flag.FlagSet {
	return flag.NewFlagSet("test", flag.ContinueOnError)
}

func TestParseConfigDefaults(t *testing.T) {
	t.Parallel()

	// url must come from somewhere (empty url is an error), so supply it via env
	// while leaving everything else to defaults.
	env := map[string]string{envTdarrUrl: "https://tdarr.example.com"}
	cfg, err := parseConfig(newFS(), nil, envFunc(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if !cfg.VerifySsl {
		t.Errorf("VerifySsl = %v, want true", cfg.VerifySsl)
	}
	if cfg.PrometheusPort != "9090" {
		t.Errorf("PrometheusPort = %q, want 9090", cfg.PrometheusPort)
	}
	if cfg.PrometheusPath != "/metrics" {
		t.Errorf("PrometheusPath = %q, want /metrics", cfg.PrometheusPath)
	}
	if cfg.HttpTimeoutSeconds != 15 {
		t.Errorf("HttpTimeoutSeconds = %d, want 15", cfg.HttpTimeoutSeconds)
	}
	if cfg.HttpMaxConcurrency != 3 {
		t.Errorf("HttpMaxConcurrency = %d, want 3", cfg.HttpMaxConcurrency)
	}
	if cfg.ApiKey != "" {
		t.Errorf("ApiKey = %q, want empty", cfg.ApiKey)
	}
	if cfg.TdarrStatsPath != "/api/v2/cruddb" {
		t.Errorf("TdarrStatsPath = %q, want /api/v2/cruddb", cfg.TdarrStatsPath)
	}
	if cfg.TdarrNodePath != "/api/v2/get-nodes" {
		t.Errorf("TdarrNodePath = %q, want /api/v2/get-nodes", cfg.TdarrNodePath)
	}
	if cfg.TdarrPieStatsPath != "/api/v2/stats/get-pies" {
		t.Errorf("TdarrPieStatsPath = %q, want /api/v2/stats/get-pies", cfg.TdarrPieStatsPath)
	}
}

func TestParseConfigEnvOverrides(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envTdarrUrl:           "https://env.example.com",
		envTdarrApiKey:        "secret-key",
		envSslVerify:          "false",
		envPrometheusPort:     "8080",
		envPrometheusPath:     "/custom",
		envLogLevel:           "debug",
		envHttpMaxConcurrency: "7",
		envHttpTimeoutSeconds: "42",
	}
	cfg, err := parseConfig(newFS(), nil, envFunc(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.url != "https://env.example.com" {
		t.Errorf("url = %q, want https://env.example.com", cfg.url)
	}
	if cfg.ApiKey != "secret-key" {
		t.Errorf("ApiKey = %q, want secret-key", cfg.ApiKey)
	}
	if cfg.VerifySsl {
		t.Errorf("VerifySsl = %v, want false", cfg.VerifySsl)
	}
	if cfg.PrometheusPort != "8080" {
		t.Errorf("PrometheusPort = %q, want 8080", cfg.PrometheusPort)
	}
	if cfg.PrometheusPath != "/custom" {
		t.Errorf("PrometheusPath = %q, want /custom", cfg.PrometheusPath)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if cfg.HttpMaxConcurrency != 7 {
		t.Errorf("HttpMaxConcurrency = %d, want 7", cfg.HttpMaxConcurrency)
	}
	if cfg.HttpTimeoutSeconds != 42 {
		t.Errorf("HttpTimeoutSeconds = %d, want 42", cfg.HttpTimeoutSeconds)
	}
}

func TestParseConfigFlagsOverrideEnv(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envTdarrUrl:           "https://env.example.com",
		envTdarrApiKey:        "env-key",
		envSslVerify:          "true",
		envPrometheusPort:     "8080",
		envPrometheusPath:     "/env",
		envLogLevel:           "warn",
		envHttpMaxConcurrency: "2",
		envHttpTimeoutSeconds: "20",
	}
	args := []string{
		"-url", "https://flag.example.com",
		"-api_key", "flag-key",
		"-verify_ssl=false",
		"-prometheus_port", "7000",
		"-prometheus_path", "/flag",
		"-log_level", "error",
		"-http_max_concurrency", "9",
		"-http_timeout_seconds", "99",
	}
	cfg, err := parseConfig(newFS(), args, envFunc(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.url != "https://flag.example.com" {
		t.Errorf("url = %q, want flag value", cfg.url)
	}
	if cfg.ApiKey != "flag-key" {
		t.Errorf("ApiKey = %q, want flag-key", cfg.ApiKey)
	}
	if cfg.VerifySsl {
		t.Errorf("VerifySsl = %v, want false (flag overrides env)", cfg.VerifySsl)
	}
	if cfg.PrometheusPort != "7000" {
		t.Errorf("PrometheusPort = %q, want 7000", cfg.PrometheusPort)
	}
	if cfg.PrometheusPath != "/flag" {
		t.Errorf("PrometheusPath = %q, want /flag", cfg.PrometheusPath)
	}
	if cfg.LogLevel != "error" {
		t.Errorf("LogLevel = %q, want error", cfg.LogLevel)
	}
	if cfg.HttpMaxConcurrency != 9 {
		t.Errorf("HttpMaxConcurrency = %d, want 9", cfg.HttpMaxConcurrency)
	}
	if cfg.HttpTimeoutSeconds != 99 {
		t.Errorf("HttpTimeoutSeconds = %d, want 99", cfg.HttpTimeoutSeconds)
	}
}

func TestParseConfigErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  map[string]string
		args []string
	}{
		{
			name: "invalid verify_ssl",
			env:  map[string]string{envTdarrUrl: "https://x.com", envSslVerify: "notabool"},
		},
		{
			name: "invalid http_max_concurrency",
			env:  map[string]string{envTdarrUrl: "https://x.com", envHttpMaxConcurrency: "notanint"},
		},
		{
			name: "invalid http_timeout_seconds",
			env:  map[string]string{envTdarrUrl: "https://x.com", envHttpTimeoutSeconds: "notanint"},
		},
		{
			name: "empty url",
			env:  map[string]string{},
		},
		{
			name: "http_max_concurrency <= 0",
			env:  map[string]string{envTdarrUrl: "https://x.com", envHttpMaxConcurrency: "0"},
		},
		{
			name: "http_timeout_seconds <= 0",
			env:  map[string]string{envTdarrUrl: "https://x.com", envHttpTimeoutSeconds: "0"},
		},
		{
			name: "invalid url",
			env:  map[string]string{envTdarrUrl: "https://exa mple.com/\x7f"},
		},
		{
			name: "unknown log level",
			env:  map[string]string{envTdarrUrl: "https://x.com", envLogLevel: "verbose"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseConfig(newFS(), tc.args, envFunc(tc.env))
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestParseConfigUrlSchemeDefaulting(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		url          string
		wantURL      string
		wantScheme   string
		wantInstance string
	}{
		{
			name:         "no scheme defaults to https",
			url:          "tdarr.example.com",
			wantURL:      "https://tdarr.example.com",
			wantScheme:   "https",
			wantInstance: "tdarr.example.com",
		},
		{
			// Scheme detection is by "://", not a "http" prefix: a scheme-less host
			// that starts with "http" must still get https:// prepended and a valid hostname.
			name:         "http-prefixed scheme-less host still defaults to https",
			url:          "http-server.lan",
			wantURL:      "https://http-server.lan",
			wantScheme:   "https",
			wantInstance: "http-server.lan",
		},
		{
			name:         "explicit http scheme preserved",
			url:          "http://tdarr.local:8080",
			wantURL:      "http://tdarr.local:8080",
			wantScheme:   "http",
			wantInstance: "tdarr.local",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{envTdarrUrl: tc.url}
			cfg, err := parseConfig(newFS(), nil, envFunc(env))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := cfg.UrlParsed.String(); got != tc.wantURL {
				t.Errorf("UrlParsed = %q, want %q", got, tc.wantURL)
			}
			if cfg.UrlParsed.Scheme != tc.wantScheme {
				t.Errorf("scheme = %q, want %q", cfg.UrlParsed.Scheme, tc.wantScheme)
			}
			if cfg.InstanceName != tc.wantInstance {
				t.Errorf("InstanceName = %q, want %q", cfg.InstanceName, tc.wantInstance)
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  zerolog.Level
	}{
		{"trace", zerolog.TraceLevel},
		{"debug", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"fatal", zerolog.FatalLevel},
		{"panic", zerolog.PanicLevel},
		{"INFO", zerolog.InfoLevel}, // case-insensitive
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := parseLogLevel(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()
		if _, err := parseLogLevel("nope"); err == nil {
			t.Fatalf("expected error for unknown level, got nil")
		}
	})
}
