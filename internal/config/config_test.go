package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewConfig(t *testing.T) {
	// test
	sampleUrl := "http://tdarr.unittest"
	t.Setenv("TDARR_URL", sampleUrl)
	c, err := NewConfig()
	defaults, defaultErr := newDefaults()
	require := require.New(t)
	require.Nil(err, "NewConfig should not return an error")
	require.Nil(defaultErr, "newDefaults should not return an error")
	require.Equal(c.url, sampleUrl)
	require.Equal(c.UrlParsed.String(), sampleUrl)
	require.Equal(c.InstanceName, "tdarr.unittest")
	require.Equal(c.ApiKey, defaults.ApiKey)
	require.Equal(c.VerifySsl, defaults.VerifySsl)
	require.Equal(c.PrometheusPort, defaults.PrometheusPort)
	require.Equal(c.PrometheusPath, defaults.PrometheusPath)
	require.Equal(c.HttpTimeoutSeconds, defaults.HttpTimeoutSeconds)
	require.Equal(c.TdarrMetricsPath, defaults.TdarrMetricsPath)
	require.Equal(c.TdarrNodePath, defaults.TdarrNodePath)
}

func TestBadConfig(t *testing.T) {
	// no url set
	require := require.New(t)
	_, err := NewConfig()
	require.NotNil(err, "NewConfig should return an error")
}

func TestEnvIntake(t *testing.T) {
	// test
	sampleUrl := "http://tdarr.unittest"
	sampleApiKey := "testApiKey"
	sampleVerifySsl := "false"
	samplePromPort := "8080"
	samplePromPath := "/doodle"
	sampleLogLevel := "debug"
	t.Setenv("TDARR_URL", sampleUrl)
	t.Setenv("TDARR_API_KEY", sampleApiKey)
	t.Setenv("VERIFY_SSL", sampleVerifySsl)
	t.Setenv("PROMETHEUS_PORT", samplePromPort)
	t.Setenv("PROMETHEUS_PATH", samplePromPath)
	t.Setenv("LOG_LEVEL", sampleLogLevel)
	c, err := NewConfig()
	require := require.New(t)
	require.Nil(err, "NewConfig should not return an error")
	require.Equal(c.url, sampleUrl)
	require.Equal(c.ApiKey, sampleApiKey)
	require.Equal(c.VerifySsl, false)
	require.Equal(c.PrometheusPort, samplePromPort)
	require.Equal(c.PrometheusPath, samplePromPath)
	require.Equal(c.LogLevel, sampleLogLevel)
}

func TestBadLogLevel(t *testing.T) {
	// not a valid log level string
	t.Setenv("TDARR_URL", "http://tdarr.unittest")
	t.Setenv("LOG_LEVEL", "doodle")
	_, err := NewConfig()
	require := require.New(t)
	require.NotNil(err, "NewConfig should return an error")
}

func TestBadBoolean(t *testing.T) {
	// not a valid boolean string
	t.Setenv("TDARR_URL", "http://tdarr.unittest")
	t.Setenv("VERIFY_SSL", "doodle")
	_, err := NewConfig()
	require := require.New(t)
	require.NotNil(err, "NewConfig should return an error")
}
