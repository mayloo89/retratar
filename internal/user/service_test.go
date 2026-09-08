package user_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
)

func TestCompleteLoginCreatesTheAccount(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	raw, err := svc.RequestLogin(t.Context(), "ana@example.com")
	if err != nil {
		t.Fatalf("RequestLogin() error = %v, want nil", err)
	}

	// Asking for a link must not create anything. Otherwise anyone can fill the
	// users table with addresses they do not own, just by asking.
	if got := countUsers(t, pool); got != 0 {
		t.Errorf("users after RequestLogin = %d, want 0", got)
	}

	u, err := svc.CompleteLogin(t.Context(), raw)
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v, want nil", err)
	}

	if u.Email != "ana@example.com" {
		t.Errorf("Email = %q, want %q", u.Email, "ana@example.com")
	}
	if u.State != user.StatePendingHandle {
		t.Errorf("State = %q, want %q", u.State, user.StatePendingHandle)
	}
	if u.Handle != "" {
		t.Errorf("Handle = %q, want empty until one is chosen", u.Handle)
	}
	if u.Tier != "free" {
		t.Errorf("Tier = %q, want %q", u.Tier, "free")
	}
	if u.ID.String() == "" {
		t.Error("ID is empty")
	}
	if u.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestTokenIsSingleUse(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	raw, err := svc.RequestLogin(t.Context(), "ana@example.com")
	if err != nil {
		t.Fatalf("RequestLogin() error = %v, want nil", err)
	}

	if _, err = svc.CompleteLogin(t.Context(), raw); err != nil {
		t.Fatalf("first CompleteLogin() error = %v, want nil", err)
	}

	_, err = svc.CompleteLogin(t.Context(), raw)
	if !errors.Is(err, user.ErrInvalidToken) {
		t.Fatalf("second CompleteLogin() error = %v, want ErrInvalidToken", err)
	}
}

// TestConcurrentCompleteLoginSpendsTheTokenOnce drives the whole service path
// the way two clicks on one link would: several requests for one token, at once.
//
// It does not prove the row lock. Released together, these goroutines still
// arrive microseconds apart, which is wide enough to miss the window between a
// read and a write — a read-then-write consume passes this test. The proof is
// store.TestConsumeLoginTokenLocksTheRow, which holds one transaction open
// across another one's attempt. This one guards the layer above it: the
// transaction boundaries, the error mapping, and the single account.
func TestConcurrentCompleteLoginSpendsTheTokenOnce(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	raw, err := svc.RequestLogin(t.Context(), "ana@example.com")
	if err != nil {
		t.Fatalf("RequestLogin() error = %v, want nil", err)
	}

	const attempts = 8
	errs := make([]error, attempts)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // Release them together, so they contend for the row.
			_, errs[i] = svc.CompleteLogin(t.Context(), raw)
		}()
	}
	close(start)
	wg.Wait()

	var succeeded int
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, user.ErrInvalidToken):
		default:
			t.Errorf("CompleteLogin() error = %v, want nil or ErrInvalidToken", err)
		}
	}

	if succeeded != 1 {
		t.Fatalf("%d of %d concurrent logins succeeded, want exactly 1", succeeded, attempts)
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	raw, err := svc.RequestLogin(t.Context(), "ana@example.com")
	if err != nil {
		t.Fatalf("RequestLogin() error = %v, want nil", err)
	}

	// Moving the deadline is the only way to test this without the test taking
	// a quarter of an hour. It also keeps the assertion honest: the expiry is
	// enforced by the database clock, and this moves it there.
	if _, err = pool.Exec(t.Context(),
		`UPDATE login_tokens SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatalf("expire token: %v", err)
	}

	_, err = svc.CompleteLogin(t.Context(), raw)
	if !errors.Is(err, user.ErrInvalidToken) {
		t.Fatalf("CompleteLogin() error = %v, want ErrInvalidToken", err)
	}
}

func TestRequestingANewLinkRetiresTheOldOne(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	first, err := svc.RequestLogin(t.Context(), "ana@example.com")
	if err != nil {
		t.Fatalf("first RequestLogin() error = %v, want nil", err)
	}

	second, err := svc.RequestLogin(t.Context(), "ana@example.com")
	if err != nil {
		t.Fatalf("second RequestLogin() error = %v, want nil", err)
	}

	if _, err := svc.CompleteLogin(t.Context(), first); !errors.Is(err, user.ErrInvalidToken) {
		t.Errorf("old token error = %v, want ErrInvalidToken", err)
	}
	if _, err := svc.CompleteLogin(t.Context(), second); err != nil {
		t.Errorf("new token error = %v, want nil", err)
	}
}

func TestSecondLoginReusesTheSameAccount(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	first := login(t, svc, "ana@example.com")
	second := login(t, svc, "ana@example.com")

	if first.ID != second.ID {
		t.Errorf("second login created a new account: %s then %s", first.ID, second.ID)
	}
	if got := countUsers(t, pool); got != 1 {
		t.Errorf("users = %d, want 1", got)
	}
}

// TestAddressCaseFoldsToOneAccount guards the property the unique index on
// lower(email) exists for. Somebody typing Ana@ on their phone, where the
// keyboard capitalises the first letter, must land in the account they made on
// their laptop.
func TestAddressCaseFoldsToOneAccount(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	lower := login(t, svc, "ana@example.com")
	upper := login(t, svc, "Ana@Example.COM")

	if lower.ID != upper.ID {
		t.Errorf("case produced two accounts: %s and %s", lower.ID, upper.ID)
	}
	if upper.Email != "ana@example.com" {
		t.Errorf("Email = %q, want the folded form", upper.Email)
	}
	if got := countUsers(t, pool); got != 1 {
		t.Errorf("users = %d, want 1", got)
	}
}

// TestTokenIsStoredOnlyAsADigest is the assertion that makes a database dump
// useless to whoever obtains it.
func TestTokenIsStoredOnlyAsADigest(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	raw, err := svc.RequestLogin(t.Context(), "ana@example.com")
	if err != nil {
		t.Fatalf("RequestLogin() error = %v, want nil", err)
	}

	var stored []byte
	if err := pool.QueryRow(t.Context(),
		`SELECT token_hash FROM login_tokens`).Scan(&stored); err != nil {
		t.Fatalf("read token_hash: %v", err)
	}

	if string(stored) == raw {
		t.Fatal("token_hash holds the token itself")
	}
	if bytes.Contains(stored, []byte(raw)) {
		t.Fatal("token_hash contains the token")
	}

	want := sha256.Sum256([]byte(raw))
	if !bytes.Equal(stored, want[:]) {
		t.Errorf("token_hash is not SHA-256 of the token")
	}
}

func TestRequestLoginRejectsUnusableAddresses(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	for _, address := range []string{"", "   ", "ana", "ana@", "@example.com", "ana@localhost"} {
		if _, err := svc.RequestLogin(t.Context(), address); !errors.Is(err, user.ErrInvalidEmail) {
			t.Errorf("RequestLogin(%q) error = %v, want ErrInvalidEmail", address, err)
		}
	}

	if got := countLoginTokens(t, pool); got != 0 {
		t.Errorf("login_tokens = %d, want 0: a rejected address must not mint one", got)
	}
}

func TestCompleteLoginRejectsRubbish(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	tokens := []string{"", "   ", "not-a-token", strings.Repeat("A", 4096)}
	for _, token := range tokens {
		if _, err := svc.CompleteLogin(t.Context(), token); !errors.Is(err, user.ErrInvalidToken) {
			t.Errorf("CompleteLogin(%.20q) error = %v, want ErrInvalidToken", token, err)
		}
	}
}

func TestGetByIDReturnsTheAccount(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	created := login(t, svc, "ana@example.com")

	got, err := svc.GetByID(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v, want nil", err)
	}
	if got.Email != created.Email {
		t.Errorf("Email = %q, want %q", got.Email, created.Email)
	}
}

func TestGetByIDRejectsUnknownID(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	if _, err := svc.GetByID(t.Context(), uuid.New()); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatalf("GetByID() error = %v, want ErrUserNotFound", err)
	}
}

func TestClaimHandleSucceeds(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	created := login(t, svc, "ana@example.com")

	got, err := svc.ClaimHandle(t.Context(), created.ID, "ana-lucia")
	if err != nil {
		t.Fatalf("ClaimHandle() error = %v, want nil", err)
	}
	if got.Handle != "ana-lucia" {
		t.Errorf("Handle = %q, want %q", got.Handle, "ana-lucia")
	}
	if got.State != user.StateActive {
		t.Errorf("State = %q, want %q", got.State, user.StateActive)
	}
}

func TestClaimHandleRejectsInvalidShape(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	created := login(t, svc, "ana@example.com")

	if _, err := svc.ClaimHandle(t.Context(), created.ID, "Ana Lucia"); !errors.Is(err, user.ErrInvalidHandle) {
		t.Fatalf("ClaimHandle() error = %v, want ErrInvalidHandle", err)
	}
}

func TestClaimHandleRejectsAlreadySet(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	created := login(t, svc, "ana@example.com")

	if _, err := svc.ClaimHandle(t.Context(), created.ID, "ana"); err != nil {
		t.Fatalf("first ClaimHandle() error = %v, want nil", err)
	}
	if _, err := svc.ClaimHandle(t.Context(), created.ID, "ana-lucia"); !errors.Is(err, user.ErrHandleAlreadySet) {
		t.Fatalf("second ClaimHandle() error = %v, want ErrHandleAlreadySet", err)
	}
}

// TestClaimHandleRejectsTaken proves the unique index on handle, not just an
// in-app check, is what stops a second account claiming one already held.
func TestClaimHandleRejectsTaken(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	first := login(t, svc, "ana@example.com")
	second := login(t, svc, "bea@example.com")

	if _, err := svc.ClaimHandle(t.Context(), first.ID, "ana"); err != nil {
		t.Fatalf("first ClaimHandle() error = %v, want nil", err)
	}
	if _, err := svc.ClaimHandle(t.Context(), second.ID, "ana"); !errors.Is(err, user.ErrHandleTaken) {
		t.Fatalf("second ClaimHandle() error = %v, want ErrHandleTaken", err)
	}
}

func TestGetByHandleReturnsTheAccount(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	created := login(t, svc, "ana@example.com")

	if _, err := svc.ClaimHandle(t.Context(), created.ID, "ana"); err != nil {
		t.Fatalf("ClaimHandle() error = %v, want nil", err)
	}

	got, err := svc.GetByHandle(t.Context(), "ana")
	if err != nil {
		t.Fatalf("GetByHandle() error = %v, want nil", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %s, want %s", got.ID, created.ID)
	}

	// Handles are DNS labels: a lookup must not care about case.
	got, err = svc.GetByHandle(t.Context(), "Ana")
	if err != nil {
		t.Fatalf("GetByHandle(uppercase) error = %v, want nil", err)
	}
	if got.ID != created.ID {
		t.Errorf("GetByHandle(uppercase) ID = %s, want %s", got.ID, created.ID)
	}
}

func TestGetByHandleRejectsUnknown(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)

	if _, err := svc.GetByHandle(t.Context(), "nobody"); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatalf("GetByHandle() error = %v, want ErrUserNotFound", err)
	}
}

// login runs a whole magic link round trip and returns the account.
func login(t *testing.T, svc *user.Service, address string) user.User {
	t.Helper()

	raw, err := svc.RequestLogin(t.Context(), address)
	if err != nil {
		t.Fatalf("RequestLogin(%q) error = %v, want nil", address, err)
	}
	u, err := svc.CompleteLogin(t.Context(), raw)
	if err != nil {
		t.Fatalf("CompleteLogin() for %q error = %v, want nil", address, err)
	}
	return u
}

func countUsers(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	return count(t, pool, "SELECT count(*) FROM users")
}

func countLoginTokens(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	return count(t, pool, "SELECT count(*) FROM login_tokens")
}

func count(t *testing.T, pool *pgxpool.Pool, query string) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}
