package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mayloo89/retratar/internal/config"
)

// requireDatabaseURL skips locally when no database is configured, and fails
// hard in CI instead — matching internal/testdb.New, since run() now needs to
// open a real pool to start. A skipped test reads as a passing one; that is a
// fair trade locally for not requiring Docker, but in CI it would mean this
// path quietly stopped being exercised.
func requireDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("DATABASE_URL is unset in CI: this test must not be skipped there")
		}
		t.Skip("DATABASE_URL unset; run `make db-up` and export it to run this test")
	}
	return url
}

// TestRunStartsAndShutsDown exercises the real startup path: config, loggers,
// both listeners, signal handling and drain. This is what the injected getenv
// and io.Writer buy — no subprocess, no global state, no sleep-and-hope.
func TestRunStartsAndShutsDown(t *testing.T) {
	url := requireDatabaseURL(t)
	ctx, cancel := context.WithCancel(t.Context())

	getenv := envFunc(map[string]string{
		"ADDR":             "127.0.0.1:0",
		"ADMIN_ADDR":       "127.0.0.1:0",
		"SHUTDOWN_TIMEOUT": "2s",
		"DATABASE_URL":     url,
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

func TestNewMailSenderRefusesProduction(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Env: config.EnvProduction}
	_, err := newMailSender(cfg, slog.New(slog.DiscardHandler))
	if !errors.Is(err, errNoProductionMailSender) {
		t.Fatalf("newMailSender() error = %v, want errNoProductionMailSender", err)
	}
}

func TestNewMailSenderAllowsDevelopment(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Env: config.EnvDevelopment}
	sender, err := newMailSender(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("newMailSender() error = %v, want nil", err)
	}
	if sender == nil {
		t.Fatal("newMailSender() returned a nil sender")
	}
}

// TestRunRejectsProductionWithoutMailSender proves the gate is actually wired
// into run(), not just reachable in isolation: production config must fail
// before run() gets anywhere near opening a database connection.
func TestRunRejectsProductionWithoutMailSender(t *testing.T) {
	t.Parallel()

	getenv := envFunc(map[string]string{
		"ENV":          "production",
		"DATABASE_URL": "postgres://retratar:retratar@127.0.0.1:5432/retratar",
	})

	err := run(t.Context(), nil, getenv, io.Discard)
	if !errors.Is(err, errNoProductionMailSender) {
		t.Fatalf("run() error = %v, want errNoProductionMailSender", err)
	}
}
