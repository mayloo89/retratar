// Package testdb gives a test its own migrated Postgres schema.
//
// It is only ever imported from tests. It lives outside _test.go so that both
// internal/store and internal/user can use it without either importing the
// other's test code.
//
// The database is real, not a fake. Everything worth asserting about this
// schema — that a single-use token is single-use under concurrency, that two
// spellings of an address collide on one index — is behaviour Postgres provides
// and a stub would simply agree with.
package testdb

import (
	"context"
	"crypto/rand"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/store"
)

const (
	// namePrefixLimit keeps the readable part of a schema name short enough
	// that the unique suffix survives Postgres's 63-byte identifier limit.
	namePrefixLimit = 40

	// suffixLength is how much randomness each schema name carries. Enough that
	// two parallel runs of the same test never collide.
	suffixLength = 10

	// dropTimeout bounds cleanup, which runs after the test's own context has
	// already been cancelled.
	dropTimeout = 30 * time.Second
)

// New returns a pool whose connections see a freshly migrated schema of their
// own, dropped when the test finishes.
//
// Isolating by schema rather than by database keeps every test on one server
// and lets them run in parallel without seeing each other's rows, which is what
// makes `go test -shuffle=on` meaningful here.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		// A skipped test reads as a passing test. Locally that is a fair trade
		// for not requiring Docker to run `go test ./...`; in CI it would mean
		// the database tests quietly stopped running and nobody noticed, so
		// there it is a failure instead.
		if os.Getenv("CI") != "" {
			t.Fatal("DATABASE_URL is unset in CI: database tests must not be skipped there")
		}
		t.Skip("DATABASE_URL unset; run `make db-up` and export it to run database tests")
	}

	ctx := t.Context()
	schema := schemaName(t)
	quoted := pgx.Identifier{schema}.Sanitize()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect to %s: %v", redact(url), err)
	}

	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		admin.Close()
		t.Fatalf("parse database url: %v", err)
	}
	// Set on the connection rather than sent as a statement: the pool opens
	// connections lazily and replaces them, and a SET on one of them would not
	// reach the others.
	cfg.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		admin.Close()
		t.Fatalf("create pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		// t.Context is already cancelled by the time cleanups run, so this
		// needs a context of its own or the schema leaks.
		dropCtx, cancel := context.WithTimeout(context.Background(), dropTimeout)
		defer cancel()
		if _, err := admin.Exec(dropCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
		admin.Close()
	})

	if err := store.Migrate(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("migrate schema %s: %v", schema, err)
	}

	return pool
}

// schemaName builds an identifier that says which test owns it and cannot
// collide with another run.
//
// The test name is truncated rather than hashed so that a schema left behind by
// a crashed run can still be traced back to the test that made it.
func schemaName(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	b.WriteString("test_")
	for _, r := range strings.ToLower(t.Name()) {
		if len(b.String()) >= namePrefixLimit {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	// Postgres truncates identifiers at 63 bytes, so the unique part goes last
	// where it cannot be cut off.
	b.WriteByte('_')
	b.WriteString(strings.ToLower(rand.Text()[:suffixLength]))
	return b.String()
}

// redact strips the credentials from a connection string so a failed connection
// can be reported without printing the password into the test log.
func redact(url string) string {
	scheme, rest, found := strings.Cut(url, "://")
	if !found {
		return "the configured database"
	}
	if _, host, hasCredentials := strings.Cut(rest, "@"); hasCredentials {
		return scheme + "://" + host
	}
	return scheme + "://" + rest
}
