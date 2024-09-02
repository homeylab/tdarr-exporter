package config

import (
	"testing"

	flag "github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestNewConfig(t *testing.T) {
	// static input
	parameters := []struct {
		name        string
		config      *Config
		shouldError bool
	}{
		{
			name: "noUrl",
			config: &Config{
				Url:      "",
				LogLevel: "info",
			},
			shouldError: true,
		},
		{
			name: "badLogLevel",
			config: &Config{
				Url:      "http://tdarr.unittest",
				LogLevel: "bad",
			},
			shouldError: true,
		},
		{
			name: "badUrl",
			config: &Config{
				Url:      "tdarr.%%.com",
				LogLevel: "debug",
			},
			shouldError: true,
		},
		{
			name: "valid",
			config: &Config{
				Url:      "http://tdarr.unittest",
				LogLevel: "warn",
			},
			shouldError: false,
		},
	}

	for _, param := range parameters {
		t.Run(param.name, func(t *testing.T) {
			require := require.New(t)
			err := param.config.validate()
			if param.shouldError {
				require.Error(err, "validate should return an error")
			} else {
				require.NoError(err, "validate should not return an error")
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	// set only required input to avoid errors
	t.Setenv("TDARR_URL", "http://localhost")
	conf, err := NewConfig()
	require := require.New(t)
	require.NoError(err, "NewConfig should not return an error")
	require.Equal("http://localhost", conf.Url, "NewConfig should set the correct URL")
	require.Equal("localhost", conf.InstanceName, "NewConfig should set the correct instance name")
	// below are defaults set in NewConfig
	require.Equal("info", conf.LogLevel, "NewConfig should set the correct log level")
	require.Equal(true, conf.VerifySsl, "NewConfig should set the correct verify_ssl")
	require.Equal("9090", conf.PrometheusPort, "NewConfig should set the correct Prometheus port")
	require.Equal("/metrics", conf.PrometheusPath, "NewConfig should set the correct Prometheus path")
	require.Equal("/api/v2/cruddb", conf.TdarrMetricsPath, "NewConfig should set the correct Tdarr metrics path")
	require.Equal("/api/v2/get-nodes", conf.TdarrNodePath, "NewConfig should set the correct Tdarr node path")
	require.Equal(15, conf.HttpTimeoutSeconds, "NewConfig should set the correct http timeout seconds")
}

func TestValidateURLParsed(t *testing.T) {
	// static input
	sampleUrl := "tdarr.unittest.org:8080"
	sampleLogLevel := "debug"
	conf := &Config{
		Url:      sampleUrl,
		LogLevel: sampleLogLevel,
	}
	err := conf.validate()
	require := require.New(t)
	require.NoError(err, "NewConfig should not return an error")
	require.Equal(sampleUrl, conf.Url, "NewConfig should set the correct URL")
	// validate that the default protocol is set
	require.Equal("https", conf.UrlParsed.Scheme, "NewConfig should set the correct URL")
	require.Equal("tdarr.unittest.org", conf.InstanceName, "NewConfig parse the instance name from URL obj")
	require.Equal(sampleLogLevel, conf.LogLevel, "NewConfig should set the correct log level")
}

func TestEnvVars(t *testing.T) {
	// static input
	sampleUrl := "http://tdarr.unittest"
	sampleApiKey := "testApiKey"
	sampleVerifySsl := "false"
	samplePromPort := "8080"
	samplePromPath := "/doodle"
	sampleLogLevel := "warn"
	t.Setenv("TDARR_URL", sampleUrl)
	t.Setenv("TDARR_API_KEY", sampleApiKey)
	t.Setenv("VERIFY_SSL", sampleVerifySsl)
	t.Setenv("PROMETHEUS_PORT", samplePromPort)
	t.Setenv("PROMETHEUS_PATH", samplePromPath)
	t.Setenv("LOG_LEVEL", sampleLogLevel)
	conf, err := NewConfig()
	require := require.New(t)
	require.NoError(err, "NewConfig should not return an error")
	require.Equal(sampleUrl, conf.Url, "NewConfig should set the correct URL")
	require.Equal(sampleApiKey, conf.ApiKey, "NewConfig should set the correct API key")
	require.Equal(false, conf.VerifySsl, "NewConfig should set the correct verify_ssl")
	require.Equal(samplePromPort, conf.PrometheusPort, "NewConfig should set the correct Prometheus port")
	require.Equal(samplePromPath, conf.PrometheusPath, "NewConfig should set the correct Prometheus path")
	require.Equal(sampleLogLevel, conf.LogLevel, "NewConfig should set the correct log level")
}

func TestValidateFlags(t *testing.T) {
	testFlags := flag.NewFlagSet("unittest", flag.ContinueOnError)
	err := InputIntake(testFlags)
	require := require.New(t)
	require.NoError(err, "InputIntake should not return an error")
	// test flags
	val, _ := testFlags.GetBool("verify_ssl")
	logLevel, _ := testFlags.GetString("log_level")
	require.NoError(err, "InputIntake should set the correct flag")
	// default = true verify_ssl by flags
	require.Equal(true, val, "InputIntake should set the correct flag")
	// default = info log_level by flags
	require.Equal("info", logLevel, "InputIntake should set the correct flag")
}
