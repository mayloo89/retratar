package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/mayloo89/retratar/internal/config"
)

// TestRunStartsAndShutsDown exercises the real startup path: config, loggers,
// both listeners, signal handling and drain. This is what the injected getenv
// and io.Writer buy — no subprocess, no global state, no sleep-and-hope.
func TestRunStartsAndShutsDown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	getenv := envFunc(map[string]string{
		"ADDR":             "127.0.0.1:0",
		"ADMIN_ADDR":       "127.0.0.1:0",
		"SHUTDOWN_TIMEOUT": "2s",
	})

	done := make(chan error, 1)
	go func() { done <- run(ctx, nil, getenv, io.Discard) }()

	// Give both listeners time to bind before asking them to stop.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after context cancellation")
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	t.Parallel()

	// Both surfaces on one registrable domain: the process must refuse.
	getenv := envFunc(map[string]string{
		"APP_HOST":   "retrat.ar",
		"PAGES_HOST": "sebas.retrat.ar",
	})

	err := run(t.Context(), nil, getenv, io.Discard)
	if !errors.Is(err, config.ErrInsecureHosts) {
		t.Fatalf("run() error = %v, want ErrInsecureHosts", err)
	}
}

func TestRunRejectsPublicAdminAddr(t *testing.T) {
	t.Parallel()

	getenv := envFunc(map[string]string{"ADMIN_ADDR": "0.0.0.0:8081"})

	err := run(t.Context(), nil, getenv, io.Discard)
	if !errors.Is(err, config.ErrInsecureHosts) {
		t.Fatalf("run() error = %v, want ErrInsecureHosts", err)
	}
}

func envFunc(pairs map[string]string) config.Getenv {
	return func(key string) string { return pairs[key] }
}
