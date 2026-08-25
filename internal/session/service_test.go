package session_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/session"
	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
)

func TestIssueThenLookupReturnsTheUser(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := session.NewService(pool)

	raw, err := svc.Issue(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v, want nil", err)
	}

	got, err := svc.Lookup(t.Context(), raw)
	if err != nil {
		t.Fatalf("Lookup() error = %v, want nil", err)
	}
	if got != u.ID {
		t.Errorf("Lookup() = %s, want %s", got, u.ID)
	}
}

func TestRevokedSessionIsRejected(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := session.NewService(pool)

	raw, err := svc.Issue(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v, want nil", err)
	}

	if err := svc.Revoke(t.Context(), raw); err != nil {
		t.Fatalf("Revoke() error = %v, want nil", err)
	}

	if _, err := svc.Lookup(t.Context(), raw); !errors.Is(err, session.ErrInvalidSession) {
		t.Fatalf("Lookup() after Revoke() error = %v, want ErrInvalidSession", err)
	}
}

// TestRevokeIsIdempotent is the property that lets a logout handler clear the
// cookie without first checking whether the session behind it was still good.
func TestRevokeIsIdempotent(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := session.NewService(pool)

	raw, err := svc.Issue(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v, want nil", err)
	}

	if err := svc.Revoke(t.Context(), raw); err != nil {
		t.Fatalf("first Revoke() error = %v, want nil", err)
	}
	if err := svc.Revoke(t.Context(), raw); err != nil {
		t.Fatalf("second Revoke() error = %v, want nil", err)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := session.NewService(pool)

	raw, err := svc.Issue(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v, want nil", err)
	}

	// Moving the deadline is the only way to test a 30-day expiry without the
	// test taking 30 days. It also keeps the assertion honest: the expiry is
	// enforced by the database clock, and this moves it there, the same way
	// user.TestExpiredTokenIsRejected does for login tokens.
	if _, err = pool.Exec(t.Context(),
		`UPDATE sessions SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatalf("expire session: %v", err)
	}

	if _, err := svc.Lookup(t.Context(), raw); !errors.Is(err, session.ErrInvalidSession) {
		t.Fatalf("Lookup() error = %v, want ErrInvalidSession", err)
	}
}

// TestSessionTokenIsStoredOnlyAsADigest is the assertion that makes a database
// dump useless to whoever obtains it.
func TestSessionTokenIsStoredOnlyAsADigest(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := session.NewService(pool)

	raw, err := svc.Issue(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v, want nil", err)
	}

	var stored []byte
	if err := pool.QueryRow(t.Context(),
		`SELECT session_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatalf("read session_hash: %v", err)
	}

	if string(stored) == raw {
		t.Fatal("session_hash holds the token itself")
	}
	if bytes.Contains(stored, []byte(raw)) {
		t.Fatal("session_hash contains the token")
	}

	want := sha256.Sum256([]byte(raw))
	if !bytes.Equal(stored, want[:]) {
		t.Errorf("session_hash is not SHA-256 of the token")
	}
}

func TestLookupRejectsRubbish(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := session.NewService(pool)

	tokens := []string{"", "   ", "not-a-session", strings.Repeat("A", 4096)}
	for _, token := range tokens {
		if _, err := svc.Lookup(t.Context(), token); !errors.Is(err, session.ErrInvalidSession) {
			t.Errorf("Lookup(%.20q) error = %v, want ErrInvalidSession", token, err)
		}
	}
}

// newAccount creates a real user row so sessions.user_id has something valid
// to reference; a session cannot exist for nobody.
func newAccount(t *testing.T, pool *pgxpool.Pool) user.User {
	t.Helper()

	svc := user.NewService(pool)
	raw, err := svc.RequestLogin(t.Context(), "ana@example.com")
	if err != nil {
		t.Fatalf("RequestLogin() error = %v, want nil", err)
	}
	u, err := svc.CompleteLogin(t.Context(), raw)
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v, want nil", err)
	}
	return u
}
