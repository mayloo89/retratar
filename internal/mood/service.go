package mood

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/store"
)

// Service sets and reads the mood a user's page shows.
type Service struct {
	pool    *pgxpool.Pool
	queries *store.Queries
}

// NewService wires a Service to a pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: store.New(pool)}
}

// SetMood validates key and note, then records it as the current mood and
// appends it to history in one transaction. A crash between the two writes
// must never leave a mood set with no record of when it changed.
func (s *Service) SetMood(ctx context.Context, userID uuid.UUID, key Key, note string) (Mood, error) {
	if !key.Valid() {
		return Mood{}, ErrInvalidMood
	}
	note, err := NormaliseNote(note)
	if err != nil {
		return Mood{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Mood{}, fmt.Errorf("begin: %w", err)
	}
	// Rollback after a successful commit is a no-op, so this is safe on
	// every path and removes the need to get the error handling right twice.
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)

	var notePtr *string
	if note != "" {
		notePtr = &note
	}

	row, err := q.UpsertMood(ctx, store.UpsertMoodParams{
		UserID:  userID,
		MoodKey: string(key),
		Note:    notePtr,
	})
	if err != nil {
		return Mood{}, fmt.Errorf("upsert mood: %w", err)
	}

	if err := q.InsertMoodHistory(ctx, store.InsertMoodHistoryParams{
		UserID:  userID,
		MoodKey: string(key),
		Note:    notePtr,
	}); err != nil {
		return Mood{}, fmt.Errorf("insert mood history: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Mood{}, fmt.Errorf("commit: %w", err)
	}

	return fromRow(row), nil
}

// CurrentMood returns the mood a user's page currently shows, or [ErrNoMood]
// if they have never set one.
func (s *Service) CurrentMood(ctx context.Context, userID uuid.UUID) (Mood, error) {
	row, err := s.queries.GetMoodByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Mood{}, ErrNoMood
		}
		return Mood{}, fmt.Errorf("get mood: %w", err)
	}
	return fromRow(row), nil
}

// fromRow converts a database row into the type the rest of the program uses.
func fromRow(row store.Mood) Mood {
	m := Mood{
		UserID:    row.UserID,
		Key:       Key(row.MoodKey),
		UpdatedAt: row.UpdatedAt.Time,
	}
	if row.Note != nil {
		m.Note = *row.Note
	}
	return m
}
