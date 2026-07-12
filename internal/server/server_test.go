package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// freePort returns an available TCP port on 127.0.0.1 by binding to :0 and
// immediately releasing it. There is a small race window before ServeHttp
// rebinds, which is acceptable for a test on the loopback interface.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host/port: %v", err)
	}
	return port
}

// waitForServer polls the address with net.Dial until it accepts a connection
// or the deadline passes. No time.Sleep loop over a fixed count.
func waitForServer(t *testing.T, addr string, deadline time.Duration) {
	t.Helper()
	stop := time.Now().Add(deadline)
	for time.Now().Before(stop) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become reachable within %s", addr, deadline)
}

// TestListenAddressJoinHostPort pins the contract ServeHttp relies on when it
// builds http.Server.Addr with net.JoinHostPort: the result is accepted by
// net.Listen for IPv4, IPv6, and the common defaults. It documents why the
// naive fmt.Sprintf("%s:%s", host, port) is wrong — that form yields an
// unparseable "too many colons" address for IPv6 hosts like "::".
// (ServeHttp's own use of the address is exercised by TestServeHttpLifecycle.)
func TestListenAddressJoinHostPort(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		wantAddr string
	}{
		{name: "ipv4 loopback", host: "127.0.0.1", wantAddr: "127.0.0.1:0"},
		{name: "ipv4 unspecified", host: "0.0.0.0", wantAddr: "0.0.0.0:0"},
		{name: "ipv6 unspecified", host: "::", wantAddr: "[::]:0"},
		{name: "ipv6 loopback", host: "::1", wantAddr: "[::1]:0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := net.JoinHostPort(tt.host, "0")
			if addr != tt.wantAddr {
				t.Fatalf("net.JoinHostPort(%q, \"0\") = %q, want %q", tt.host, addr, tt.wantAddr)
			}

			ln, err := net.Listen("tcp", addr)
			if err != nil {
				if strings.Contains(tt.host, ":") {
					t.Skipf("skipping IPv6 bind: environment appears to lack IPv6 support: %v", err)
				}
				t.Fatalf("net.Listen(%q) failed: %v", addr, err)
			}
			defer func() { _ = ln.Close() }()
		})
	}
}

func TestServeHttpLifecycle(t *testing.T) {
	port := freePort(t)
	addr := net.JoinHostPort("127.0.0.1", port)

	wg := &sync.WaitGroup{}
	stopChan := make(chan bool)
	errChan := make(chan error, 1)
	cfg := HttpServerConfig{
		TdarrInstance:   "lifecycle",
		ListenAddress:   "127.0.0.1",
		PrometheusPort:  port,
		PrometheusPath:  "/metrics",
		GracefulTimeout: 5 * time.Second,
	}

	wg.Add(1)
	go ServeHttp(wg, prometheus.NewRegistry(), cfg, stopChan, errChan)

	waitForServer(t, addr, 2*time.Second)

	// Confirm it actually serves a route.
	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// No spurious error should have been delivered while serving.
	select {
	case err := <-errChan:
		t.Fatalf("unexpected error during normal serving: %v", err)
	default:
	}

	// Trigger graceful shutdown and assert the WaitGroup completes (i.e.
	// ServeHttp returned via wg.Done, not os.Exit).
	stopChan <- true

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeHttp did not return / WaitGroup did not complete after shutdown")
	}

	// A clean shutdown must not deliver an error.
	select {
	case err := <-errChan:
		t.Fatalf("unexpected error on clean shutdown: %v", err)
	default:
	}
}

func TestServeHttpListenErrorDeliveredOnChannel(t *testing.T) {
	// Occupy a port so ListenAndServe fails with "address already in use"
	// instead of crashing the process via log.Fatal/os.Exit.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to occupy port: %v", err)
	}
	defer func() { _ = occupied.Close() }()
	_, port, err := net.SplitHostPort(occupied.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host/port: %v", err)
	}

	wg := &sync.WaitGroup{}
	stopChan := make(chan bool, 1)
	errChan := make(chan error, 1)
	cfg := HttpServerConfig{
		TdarrInstance:   "listen-error",
		ListenAddress:   "127.0.0.1",
		PrometheusPort:  port,
		PrometheusPath:  "/metrics",
		GracefulTimeout: 1 * time.Second,
	}

	wg.Add(1)
	go ServeHttp(wg, prometheus.NewRegistry(), cfg, stopChan, errChan)

	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("expected a non-nil ListenAndServe error on errChan")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ListenAndServe error was not delivered on errChan")
	}

	// ServeHttp is still blocked on stopChan after the listen error (matching
	// production wiring where main sends stop after receiving the error).
	// Release it and confirm clean return.
	stopChan <- true

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ServeHttp did not return after stop following listen error")
	}
}
