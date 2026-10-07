package guestbook

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/store"
)

// Service signs and reads guestbooks.
type Service struct {
	queries *store.Queries
}

// NewService wires a Service to a pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{queries: store.New(pool)}
}

// Sign validates body and stores it as a visible entry on pageUserID's
// guestbook. Signing your own guestbook is [ErrOwnPage].
func (s *Service) Sign(ctx context.Context, pageUserID, authorUserID uuid.UUID, body string) (Entry, error) {
	if pageUserID == authorUserID {
		return Entry{}, ErrOwnPage
	}
	body, err := NormaliseBody(body)
	if err != nil {
		return Entry{}, err
	}
	row, err := s.queries.CreateGuestbookEntry(ctx, store.CreateGuestbookEntryParams{
		PageUserID:   pageUserID,
		AuthorUserID: authorUserID,
		Body:         body,
	})
	if err != nil {
		return Entry{}, fmt.Errorf("create guestbook entry: %w", err)
	}
	return Entry{ID: row.ID, Body: row.Body, CreatedAt: row.CreatedAt.Time}, nil
}

// Visible returns up to limit visible entries on pageUserID's guestbook,
// newest first.
func (s *Service) Visible(ctx context.Context, pageUserID uuid.UUID, limit int) ([]Entry, error) {
	rows, err := s.queries.ListVisibleGuestbookEntries(ctx, store.ListVisibleGuestbookEntriesParams{
		PageUserID: pageUserID,
		Lim:        int32(limit), //nolint:gosec // limit is PageLimit, a small constant
	})
	if err != nil {
		return nil, fmt.Errorf("list guestbook entries: %w", err)
	}
	entries := make([]Entry, len(rows))
	for i, row := range rows {
		entries[i] = Entry{ID: row.ID, Body: row.Body, CreatedAt: row.CreatedAt.Time}
		if row.AuthorHandle != nil {
			entries[i].AuthorHandle = *row.AuthorHandle
		}
	}
	return entries, nil
}
