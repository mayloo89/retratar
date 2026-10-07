package guestbook

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/store"
)

// The values of guestbook_entries.state.
const (
	stateVisible = "visible"
	stateHidden  = "hidden"
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

// Own returns up to limit entries on pageUserID's guestbook in any state,
// newest first, for the owner to moderate.
func (s *Service) Own(ctx context.Context, pageUserID uuid.UUID, limit int) ([]Entry, error) {
	rows, err := s.queries.ListOwnGuestbookEntries(ctx, store.ListOwnGuestbookEntriesParams{
		PageUserID: pageUserID,
		Lim:        int32(limit), //nolint:gosec // limit is OwnerLimit, a small constant
	})
	if err != nil {
		return nil, fmt.Errorf("list own guestbook entries: %w", err)
	}
	entries := make([]Entry, len(rows))
	for i, row := range rows {
		entries[i] = Entry{
			ID:        row.ID,
			Body:      row.Body,
			CreatedAt: row.CreatedAt.Time,
			Hidden:    row.State == stateHidden,
		}
		if row.AuthorHandle != nil {
			entries[i].AuthorHandle = *row.AuthorHandle
		}
	}
	return entries, nil
}

// Hide takes an entry off pageUserID's public page. An entry that is not on
// that page is [ErrEntryNotFound].
func (s *Service) Hide(ctx context.Context, pageUserID, entryID uuid.UUID) error {
	return s.setState(ctx, pageUserID, entryID, stateHidden)
}

// Show puts a hidden entry back on pageUserID's public page. An entry that is
// not on that page is [ErrEntryNotFound].
func (s *Service) Show(ctx context.Context, pageUserID, entryID uuid.UUID) error {
	return s.setState(ctx, pageUserID, entryID, stateVisible)
}

func (s *Service) setState(ctx context.Context, pageUserID, entryID uuid.UUID, state string) error {
	n, err := s.queries.SetGuestbookEntryState(ctx, store.SetGuestbookEntryStateParams{
		State:      state,
		ID:         entryID,
		PageUserID: pageUserID,
	})
	if err != nil {
		return fmt.Errorf("set guestbook entry state: %w", err)
	}
	if n == 0 {
		return ErrEntryNotFound
	}
	return nil
}
