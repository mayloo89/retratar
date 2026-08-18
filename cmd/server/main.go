// Command server runs retratar: the owner-facing app and the public pages,
// one binary, split by hostname.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mayloo89/retratar/internal/config"
	"github.com/mayloo89/retratar/internal/web"
)

func main() {
	if err := run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// run holds the whole program so that every exit path returns an error instead
// of calling os.Exit, which makes the startup path testable.
func run(ctx context.Context, out *os.File) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg, out)
	logger.Info("starting",
		slog.String("env", string(cfg.Env)),
		slog.String("addr", cfg.Addr),
		slog.String("app_host", cfg.AppHost),
		slog.String("pages_host", cfg.PagesHost),
	)

	srv := &web.Server{Config: cfg, Logger: logger}

	httpServer := &http.Server{
		Addr:    cfg.Addr,
		Handler: srv.Handler(),
		// Every timeout is set. An unbounded read or an idle connection that
		// never closes is how a single client exhausts the server.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 16, // 64 KiB
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("listen: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down", slog.Duration("timeout", cfg.ShutdownTimeout))
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("stopped")
	return nil
}

// newLogger returns structured JSON in production, and human-readable text
// locally.
func newLogger(cfg config.Config, out *os.File) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if cfg.IsProduction() {
		return slog.New(slog.NewJSONHandler(out, opts))
	}
	opts.Level = slog.LevelDebug
	return slog.New(slog.NewTextHandler(out, opts))
}
