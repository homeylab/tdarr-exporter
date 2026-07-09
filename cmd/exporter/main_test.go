package main

import (
	"errors"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
)

// TestAwaitShutdown verifies the exit-code contract: an OS signal yields 0, a
// fatal HTTP server error yields 1. This guards the regression fixed by
// splitting main into run() int (previously defer os.Exit(0) forced 0).
func TestAwaitShutdown(t *testing.T) {
	t.Parallel()

	t.Run("signal shutdown exits 0", func(t *testing.T) {
		t.Parallel()
		quit := make(chan os.Signal, 1)
		quit <- os.Interrupt
		if got := awaitShutdown(quit, make(chan error)); got != 0 {
			t.Errorf("awaitShutdown on signal = %d, want 0", got)
		}
	})

	t.Run("server error shutdown exits 1", func(t *testing.T) {
		t.Parallel()
		errCh := make(chan error, 1)
		errCh <- errors.New("listen: address already in use")
		if got := awaitShutdown(make(chan os.Signal), errCh); got != 1 {
			t.Errorf("awaitShutdown on server error = %d, want 1", got)
		}
	})
}

// TestBuildInfoCarriesInstanceLabel verifies tdarr_exporter_build_info is
// registered through the tdarr_instance-labeled registerer (mirroring run()),
// so the exporter's build-info metric is labeled like the rest of its metrics.
func TestBuildInfoCarriesInstanceLabel(t *testing.T) {
	t.Parallel()

	const wantInstance = "tdarr-4k"
	registry := prometheus.NewRegistry()
	prometheus.WrapRegistererWith(
		prometheus.Labels{"tdarr_instance": wantInstance},
		registry,
	).MustRegister(versioncollector.NewCollector("tdarr_exporter"))

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("registry.Gather: %v", err)
	}

	var found bool
	for _, fam := range families {
		if fam.GetName() != "tdarr_exporter_build_info" {
			continue
		}
		found = true
		metrics := fam.GetMetric()
		if len(metrics) != 1 {
			t.Fatalf("tdarr_exporter_build_info: want 1 metric, got %d", len(metrics))
		}
		var gotInstance string
		var hasLabel bool
		for _, lp := range metrics[0].GetLabel() {
			if lp.GetName() == "tdarr_instance" {
				hasLabel = true
				gotInstance = lp.GetValue()
			}
		}
		if !hasLabel {
			t.Errorf("tdarr_exporter_build_info: missing tdarr_instance label")
		}
		if gotInstance != wantInstance {
			t.Errorf("tdarr_instance: want %q, got %q", wantInstance, gotInstance)
		}
	}
	if !found {
		t.Fatal("tdarr_exporter_build_info family not found in gathered metrics")
	}
}
