package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/mayloo89/retratar/migrations"
)

// Migrate applies every migration that has not run yet and reports what it did.
//
// It is called from the -migrate subcommand and from tests, never from the
// normal start-up path. Migrating on boot means a restart at the wrong moment
// changes the schema, and a rolled-back binary meets a database that has moved
// on without it. Changing the shape of the data should take somebody deciding
// to change it.
func Migrate(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	// Borrows the pool rather than dialling again, so migrations reach the same
	// database the application will use, resolved the same way. Closing this
	// handle does not close the pool.
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	// The provider API keeps the configuration on this value. goose also has a
	// set of package-level functions backed by global state; using those would
	// make two callers in one process — the server and a test — quietly share
	// settings.
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	for _, r := range results {
		logger.Info("migration applied",
			slog.Int64("version", r.Source.Version),
			slog.String("source", r.Source.Path),
			slog.Duration("took", r.Duration),
		)
	}
	logger.Info("migrations up to date", slog.Int("applied", len(results)))

	return nil
}
