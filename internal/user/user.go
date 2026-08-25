// Package user owns accounts and the magic link login that creates them.
//
// There are no passwords anywhere in this package, and there is no separate
// signup: proving you can read an email address both creates the account and
// signs you in. The two operations that matter are [Service.RequestLogin],
// which mints a link, and [Service.CompleteLogin], which spends one.
//
// This package deliberately stops at the account. Issuing the session cookie
// belongs to the web layer, and sending the email belongs to a mailer; neither
// is imported here.
package user

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/store"
)

// TokenTTL is how long a magic link stays usable.
//
// Short enough that a link left sitting in an inbox, or forwarded by accident,
// stops being a credential quickly. Long enough to survive a slow mail relay
// and somebody switching to their laptop to open it.
const TokenTTL = 15 * time.Minute

// maxEmailLength is the longest address SMTP is required to carry.
const maxEmailLength = 254

// State is where an account sits in its lifecycle.
type State string

const (
	// StatePendingHandle is an account that exists but has not picked the
	// handle its page will live on. It is the state every account starts in.
	StatePendingHandle State = "pending_handle"

	// StateActive is an account with a handle.
	StateActive State = "active"
)

// User is an account.
//
// Handle is empty until the owner chooses one, which is why it is a string here
// and nullable in the database: absent and empty mean the same thing to every
// caller, and a pointer would push that distinction into code that does not
// care about it.
type User struct {
	ID        uuid.UUID
	Email     string
	Handle    string
	State     State
	Tier      string
	Locale    string
	CreatedAt time.Time
}

// Service issues and redeems magic links.
type Service struct {
	pool    *pgxpool.Pool
	queries *store.Queries
	ttl     time.Duration
}

// NewService wires a Service to a pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: store.New(pool), ttl: TokenTTL}
}

// RequestLogin mints a magic link token for an address and returns it.
//
// The returned string is the only copy that ever leaves this function; the
// database holds a digest. Put it in a link, mail it, and forget it.
//
// It does not create an account and does not report whether one exists. A
// caller that reveals the difference — a different response for a known
// address, or a faster one — hands out a list of who has signed up.
func (s *Service) RequestLogin(ctx context.Context, email string) (string, error) {
	address, err := NormaliseEmail(email)
	if err != nil {
		return "", err
	}

	raw, digest := newToken()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	// Rollback after a successful commit is a no-op, so this is safe on every
	// path and removes the need to get the error handling right twice.
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)

	// Retiring the outstanding tokens and issuing the new one share a
	// transaction so that a failure cannot leave an address with no working
	// link and no way to tell.
	if err := q.InvalidateLoginTokensForEmail(ctx, address); err != nil {
		return "", fmt.Errorf("invalidate outstanding tokens: %w", err)
	}

	if err := q.CreateLoginToken(ctx, store.CreateLoginTokenParams{
		TokenHash:  digest,
		Email:      address,
		TtlSeconds: s.ttl.Seconds(),
	}); err != nil {
		return "", fmt.Errorf("create login token: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	return raw, nil
}

// CompleteLogin spends a magic link token and returns the account it belongs
// to, creating that account if this is its first login.
//
// Every way of failing returns [ErrInvalidToken]. See the comment on that error
// for why they are not distinguished.
func (s *Service) CompleteLogin(ctx context.Context, raw string) (User, error) {
	if !plausibleToken(raw) {
		return User{}, ErrInvalidToken
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)

	// Spending the token and creating the account share a transaction: a crash
	// between them would otherwise burn a single-use token and leave the person
	// who clicked it with no account and no way to retry.
	address, err := q.ConsumeLoginToken(ctx, hash(raw))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrInvalidToken
		}
		return User{}, fmt.Errorf("consume login token: %w", err)
	}

	row, err := q.UpsertUserByEmail(ctx, address)
	if err != nil {
		return User{}, fmt.Errorf("upsert user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit: %w", err)
	}

	return fromRow(row), nil
}

// GetByID returns the account a session belongs to. It is how the web layer
// turns a resolved session into the account a request runs as, without
// reaching into store.User itself.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (User, error) {
	row, err := s.queries.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("get user by id: %w", err)
	}
	return fromRow(row), nil
}

// NormaliseEmail folds an address to the single form used as the login
// identity, or reports that it is not usable.
//
// Case is folded across the whole address, local part included. RFC 5321 says
// the local part is case-sensitive and the owner of the domain decides; no
// mail provider anyone actually uses has ever decided differently, and treating
// Ana@ and ana@ as two accounts would produce a duplicate nobody asked for and
// a login that works on Tuesday and not Wednesday.
//
// The checks here are shallow on purpose. Full RFC 5322 validation accepts
// addresses no mail server will deliver to and rejects ones that work; the real
// verification is that a link sent to the address gets clicked.
func NormaliseEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > maxEmailLength {
		return "", ErrInvalidEmail
	}

	parsed, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", ErrInvalidEmail
	}

	// ParseAddress happily accepts `Ana <ana@example.com>`. Accepting the
	// display name would let two spellings of one address look like two
	// accounts, and would put attacker-chosen text into anything that echoes
	// what was typed.
	if parsed.Name != "" {
		return "", ErrInvalidEmail
	}

	address := strings.ToLower(parsed.Address)

	// ParseAddress also accepts `ana@localhost`, which is valid and useless
	// here: a magic link has to leave the building.
	_, domain, found := strings.Cut(address, "@")
	if !found || !strings.Contains(domain, ".") ||
		strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", ErrInvalidEmail
	}

	return address, nil
}

// fromRow converts a database row into the type the rest of the program uses.
func fromRow(row store.User) User {
	u := User{
		ID:        row.ID,
		Email:     row.Email,
		State:     State(row.State),
		Tier:      row.Tier,
		Locale:    row.Locale,
		CreatedAt: row.CreatedAt.Time,
	}
	if row.Handle != nil {
		u.Handle = *row.Handle
	}
	return u
}
