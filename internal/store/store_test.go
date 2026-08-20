package store_test

import (
	"crypto/sha256"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mayloo89/retratar/internal/store"
	"github.com/mayloo89/retratar/internal/testdb"
)

// contentionWindow is how long the second transaction is given to prove it is
// blocked. Only a lower bound matters: a query waiting on a lock does not come
// back early, so a slow machine cannot turn this into a false failure.
const contentionWindow = 250 * time.Millisecond

// TestMigrateCreatesTheSchema is the check PLAN.md asks for: the migrations run
// forward against a real Postgres, not a parser or a linter's idea of one.
//
// testdb.New has already migrated the schema by the time this body runs, so a
// migration that fails to apply fails here before a single assertion.
func TestMigrateCreatesTheSchema(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)

	relations := []string{
		"users",
		"login_tokens",
		"users_email_key",
		"users_handle_key",
		"login_tokens_hash_key",
		"login_tokens_email_idx",
		// goose's own bookkeeping. Its absence would mean migrations ran
		// somewhere other than this test's schema.
		"goose_db_version",
	}

	for _, name := range relations {
		var exists bool
		// to_regclass resolves through search_path, so this asserts the
		// relation landed in this test's schema and not in public.
		err := pool.QueryRow(t.Context(),
			`SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists)
		if err != nil {
			t.Fatalf("look up %s: %v", name, err)
		}
		if !exists {
			t.Errorf("relation %s does not exist after migrating", name)
		}
	}
}

// TestMigrateIsIdempotent covers the ordinary case of a deploy that migrates
// when there is nothing to migrate.
func TestMigrateIsIdempotent(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)

	if err := store.Migrate(t.Context(), pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("second Migrate() error = %v, want nil", err)
	}
}

// TestConsumeLoginTokenLocksTheRow is the test that justifies writing the
// consume step as a single UPDATE.
//
// The service-level concurrency test cannot prove this on its own: the window
// between reading a row and updating it is a few microseconds wide, and
// goroutines released together still arrive far enough apart to miss it. So
// drive the two transactions by hand, and hold the first one open across the
// second one's whole attempt.
//
// Written as read-then-write, the second transaction blocks on the row lock as
// it does here, and then comes back with the email anyway, because its own
// check ran against the row as it looked before the first commit. One token,
// two people signed in.
func TestConsumeLoginTokenLocksTheRow(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	ctx := t.Context()
	queries := store.New(pool)

	digest := sha256.Sum256([]byte("a token nobody sent"))
	err := queries.CreateLoginToken(ctx, store.CreateLoginTokenParams{
		TokenHash:  digest[:],
		Email:      "ana@example.com",
		TtlSeconds: 900,
	})
	if err != nil {
		t.Fatalf("CreateLoginToken() error = %v, want nil", err)
	}

	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v, want nil", err)
	}
	defer func() { _ = first.Rollback(ctx) }()

	if _, err := store.New(first).ConsumeLoginToken(ctx, digest[:]); err != nil {
		t.Fatalf("first ConsumeLoginToken() error = %v, want nil", err)
	}

	// The second attempt runs in a goroutine because it is expected to block on
	// the lock the first one holds, and a blocked query never returns.
	second := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			second <- err
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		_, err = store.New(tx).ConsumeLoginToken(ctx, digest[:])
		second <- err
	}()

	select {
	case err := <-second:
		t.Fatalf("second ConsumeLoginToken() returned %v while the first transaction was still open: the statement took no row lock", err)
	case <-time.After(contentionWindow):
		// Still blocked, which is the point.
	}

	if err := first.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v, want nil", err)
	}

	switch err := <-second; {
	case errors.Is(err, pgx.ErrNoRows):
		// The token was spent, so the second transaction matched nothing.
	case err == nil:
		t.Fatal("the same token was consumed twice")
	default:
		t.Fatalf("second ConsumeLoginToken() error = %v, want pgx.ErrNoRows", err)
	}
}
