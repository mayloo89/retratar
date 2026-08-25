// Package store is the database layer: the connection pool, the migration
// runner, and the query code sqlc generates from internal/store/queries.
//
// Nothing in here knows what a user is. Feature packages own their meaning;
// this package owns the fact that it is kept in Postgres.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open builds a connection pool and proves it can reach the database.
//
// The ping is not ceremony. Without it a bad DATABASE_URL surfaces as a failed
// request from a server that reported itself healthy at boot, which is a much
// worse thing to debug than a process that refuses to start.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
