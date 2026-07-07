package main

import (
	"errors"
	"os"
	"testing"
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
