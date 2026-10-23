package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mayloo89/retratar/internal/config"
	"github.com/mayloo89/retratar/internal/mail"
	"github.com/mayloo89/retratar/internal/testdb"
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
		"MAIL_SENDER":      "log",
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

// TestStatsFlag proves -stats prints the report and returns, instead of
// starting the listeners: a run that started serving would block until the
// timeout below and report ctx's deadline.
func TestStatsFlag(t *testing.T) {
	// The report reads real tables, so point run at a migrated schema of its
	// own: the shared public schema is not migrated in CI.
	pool := testdb.New(t)
	u, err := url.Parse(requireDatabaseURL(t))
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", pool.Config().ConnConfig.RuntimeParams["search_path"])
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	getenv := envFunc(map[string]string{
		"ADDR":         "127.0.0.1:0",
		"DATABASE_URL": u.String(),
		"MAIL_SENDER":  "log",
	})

	var out bytes.Buffer
	if err = run(ctx, []string{"-stats"}, getenv, &out); err != nil {
		t.Fatalf("run(-stats) error = %v, want nil", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("run(-stats) did not return before the deadline; it should not serve")
	}
	if !strings.HasPrefix(out.String(), "retratar stats at ") {
		t.Errorf("output = %q, want it to start with the stats heading", out.String())
	}
	// The schema is fresh, so a real report line carries a zero.
	if want := "\naccounts" + strings.Repeat(" ", 12) + "0\n"; !strings.Contains(out.String(), want) {
		t.Errorf("output = %q, want it to contain the line %q", out.String(), want)
	}
	if strings.Contains(out.String(), "listening") {
		t.Errorf("output mentions listening: %q", out.String())
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

// TestRunRejectsDevelopmentWithoutMailSender proves the opt-in for logging
// magic links is wired into run(): a process with no ENV and no MAIL_SENDER,
// the shape of a production box that lost its environment, must not start.
func TestRunRejectsDevelopmentWithoutMailSender(t *testing.T) {
	t.Parallel()

	// Bounded so a regressed check fails fast instead of serving until the
	// suite timeout.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err := run(ctx, nil, envFunc(nil), io.Discard)
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("run() error = %v, want ErrInvalidConfig", err)
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

func TestNewMailSenderAllowsDevelopment(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Env: config.EnvDevelopment}
	sender := newMailSender(cfg, slog.New(slog.DiscardHandler))
	if sender == nil {
		t.Fatal("newMailSender() returned a nil sender")
	}
	if _, ok := sender.(*mail.LogSender); !ok {
		t.Fatalf("newMailSender() = %T, want *mail.LogSender", sender)
	}
}

func TestNewMailSenderUsesSMTPInProduction(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Env:          config.EnvProduction,
		SMTPHost:     "smtp.postmarkapp.com",
		SMTPPort:     "587",
		SMTPUsername: "token",
		SMTPPassword: "token",
		MailFrom:     "noreply@retratar.com.ar",
	}
	sender := newMailSender(cfg, slog.New(slog.DiscardHandler))
	if sender == nil {
		t.Fatal("newMailSender() returned a nil sender")
	}
	if _, ok := sender.(*mail.SMTPSender); !ok {
		t.Fatalf("newMailSender() = %T, want *mail.SMTPSender", sender)
	}
}

// TestRunRejectsProductionWithoutSMTPConfig proves the gate is actually wired
// into run() through config.Validate, not just reachable in isolation:
// production config missing a relay must fail before run() gets anywhere near
// opening a database connection.
func TestRunRejectsProductionWithoutSMTPConfig(t *testing.T) {
	t.Parallel()

	getenv := envFunc(map[string]string{
		"ENV":          "production",
		"DATABASE_URL": "postgres://retratar:retratar@127.0.0.1:5432/retratar",
	})

	err := run(t.Context(), nil, getenv, io.Discard)
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("run() error = %v, want ErrInvalidConfig", err)
	}
}
