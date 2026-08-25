// Command server runs retratar: the owner-facing app and the public pages,
// one binary, split by hostname.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mayloo89/retratar/internal/buildinfo"
	"github.com/mayloo89/retratar/internal/config"
	"github.com/mayloo89/retratar/internal/mail"
	"github.com/mayloo89/retratar/internal/session"
	"github.com/mayloo89/retratar/internal/store"
	"github.com/mayloo89/retratar/internal/user"
	"github.com/mayloo89/retratar/internal/web"
)

// errNoProductionMailSender guards against starting production with the
// development log-based mailer: a magic link written to the log turns the
// log stream into a credential store. See [mail.LogSender].
var errNoProductionMailSender = errors.New("no mail sender configured for production")

// newMailSender chooses the mail.Sender the process runs with.
//
// There is only one implementation so far, and it is not safe in production.
// This is checked before anything opens a database connection, so refusing to
// run with it is a config-time failure, not something that shows up later
// during startup.
func newMailSender(cfg config.Config, logger *slog.Logger) (mail.Sender, error) {
	if cfg.IsProduction() {
		return nil, errNoProductionMailSender
	}
	return mail.NewLogSender(logger), nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// run holds the whole program. Every dependency on the outside world arrives
// as a parameter and every exit path returns an error instead of calling
// os.Exit, so a test can run the real startup path against a fake environment.
func run(ctx context.Context, args []string, getenv config.Getenv, stdout io.Writer) error {
	// Flags are parsed here rather than in main so that a test can drive the
	// real startup path with a chosen argument list. args excludes the program
	// name; main passes os.Args[1:].
	flags := flag.NewFlagSet("server", flag.ContinueOnError)
	flags.SetOutput(stdout)
	migrate := flags.Bool("migrate", false, "apply pending database migrations and exit")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg, stdout)

	if *migrate {
		return applyMigrations(ctx, cfg, logger)
	}

	logger.Info("starting",
		slog.String("build", buildinfo.Get().String()),
		slog.String("env", string(cfg.Env)),
		slog.String("addr", cfg.Addr),
		slog.String("app_host", cfg.AppHost),
		slog.String("pages_host", cfg.PagesHost),
	)

	mailer, err := newMailSender(cfg, logger)
	if err != nil {
		return fmt.Errorf("configure mail sender: %w", err)
	}

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	srv := &web.Server{
		Config:   cfg,
		Logger:   logger,
		Users:    user.NewService(pool),
		Sessions: session.NewService(pool),
		Mailer:   mailer,
	}

	public := newHTTPServer(cfg.Addr, srv.Handler(), logger)

	servers := []*http.Server{public}
	if cfg.AdminAddr != "" {
		// Diagnostics live on their own loopback listener. They must never
		// share a mux or a socket with the public surface.
		servers = append(servers, newHTTPServer(cfg.AdminAddr, web.AdminHandler(), logger))
	}

	return serve(ctx, logger, cfg.ShutdownTimeout, servers...)
}

// applyMigrations brings the schema up to date and returns. It is the whole of
// the -migrate subcommand.
//
// Serving and migrating are separate runs of the binary on purpose. A deploy
// migrates, then starts; a restart on its own never changes the schema, so a
// process that comes back up after a crash cannot rewrite the database while
// nobody is watching.
func applyMigrations(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	return store.Migrate(ctx, pool, logger)
}

// newHTTPServer applies the timeouts every server needs. An unbounded read or
// an idle connection that never closes is how one client exhausts the process.
func newHTTPServer(addr string, h http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 16, // 64 KiB
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
}

// serve runs every server until one fails or ctx is cancelled, then drains all
// of them within the shutdown budget.
func serve(ctx context.Context, logger *slog.Logger, timeout time.Duration, servers ...*http.Server) error {
	errCh := make(chan error, len(servers))

	for _, s := range servers {
		go func() {
			logger.Info("listening", slog.String("addr", s.Addr))
			err := s.ListenAndServe()
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			errCh <- err
		}()
	}

	var runErr error
	select {
	case runErr = <-errCh:
		if runErr != nil {
			runErr = fmt.Errorf("listen: %w", runErr)
		}
	case <-ctx.Done():
		logger.Info("shutting down", slog.Duration("timeout", timeout))
	}

	// context.WithoutCancel: ctx is already cancelled on the signal path, and
	// a cancelled context would make Shutdown return before draining anything.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		shutErrs []error
	)
	for _, s := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Shutdown(shutdownCtx); err != nil {
				mu.Lock()
				shutErrs = append(shutErrs, fmt.Errorf("shutdown %s: %w", s.Addr, err))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if err := errors.Join(append(shutErrs, runErr)...); err != nil {
		return err
	}
	logger.Info("stopped")
	return nil
}

// newLogger returns structured JSON in production, and human-readable text
// locally.
func newLogger(cfg config.Config, out io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if cfg.IsProduction() {
		return slog.New(slog.NewJSONHandler(out, opts))
	}
	opts.Level = slog.LevelDebug
	return slog.New(slog.NewTextHandler(out, opts))
}
