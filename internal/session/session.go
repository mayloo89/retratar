// Package session issues, resolves and revokes logged-in sessions.
//
// A session is a database row, not a signed cookie: the client holds a random
// token, the database holds only its SHA-256 digest, and the two never mean
// the same thing to a stolen database dump. That is also why revocation is
// real here — a signed cookie cannot be taken back once handed out.
//
// Sessions are their own package, separate from [github.com/mayloo89/retratar/internal/user],
// because they outlive login: this is where a settings page will later list
// and revoke them one at a time.
//
// The web layer sets and reads the cookie; this package never imports
// net/http, matching the boundary internal/user draws around net/mail.
package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/store"
)

// TTL is how long a session stays valid after it is issued.
//
// It is absolute, not sliding: nothing pushes expires_at forward while the
// session is used. A stolen session therefore ages out on a schedule its
// owner never has to notice or act on, instead of staying alive indefinitely
// because someone else keeps using it.
const TTL = 30 * 24 * time.Hour

// Service issues, resolves and revokes sessions.
type Service struct {
	pool    *pgxpool.Pool
	queries *store.Queries
	ttl     time.Duration
}

// NewService wires a Service to a pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: store.New(pool), ttl: TTL}
}

// Issue creates a session for a user and returns the token to put in their
// cookie.
//
// The returned string is the only copy that ever leaves this function; the
// database holds a digest.
func (s *Service) Issue(ctx context.Context, userID uuid.UUID) (string, error) {
	raw, digest := newToken()

	if err := s.queries.CreateSession(ctx, store.CreateSessionParams{
		SessionHash: digest,
		UserID:      userID,
		TtlSeconds:  s.ttl.Seconds(),
	}); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	return raw, nil
}

// Lookup resolves a session token to the user it belongs to.
//
// It returns [ErrInvalidSession] for a token that never existed, has expired,
// or was revoked; see that error's comment for why those stay merged.
func (s *Service) Lookup(ctx context.Context, raw string) (uuid.UUID, error) {
	if !plausibleToken(raw) {
		return uuid.UUID{}, ErrInvalidSession
	}

	userID, err := s.queries.LookupSession(ctx, hash(raw))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.UUID{}, ErrInvalidSession
		}
		return uuid.UUID{}, fmt.Errorf("lookup session: %w", err)
	}

	return userID, nil
}

// Revoke ends a session so its token no longer authenticates anyone.
//
// Revoking a token that is already invalid is not an error: logout should
// look like it worked whether or not the session was still live, so a caller
// clearing a cookie never needs to check whether the session behind it was
// still good.
func (s *Service) Revoke(ctx context.Context, raw string) error {
	if !plausibleToken(raw) {
		return nil
	}

	if err := s.queries.RevokeSession(ctx, hash(raw)); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	return nil
}
