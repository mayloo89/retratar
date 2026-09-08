package mood_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
)

// newAccount creates a real account, the same way
// internal/session/service_test.go does — moods.user_id references users(id),
// so every test needs a real row there.
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

func TestCurrentMoodBeforeAnySetIsErrNoMood(t *testing.T) {
	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := mood.NewService(pool)

	_, err := svc.CurrentMood(t.Context(), u.ID)
	if !errors.Is(err, mood.ErrNoMood) {
		t.Fatalf("CurrentMood() error = %v, want ErrNoMood", err)
	}
}

func TestSetMoodThenCurrentMood(t *testing.T) {
	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := mood.NewService(pool)

	if _, err := svc.SetMood(t.Context(), u.ID, mood.KeyFeliz, "todo bien"); err != nil {
		t.Fatalf("SetMood() error = %v, want nil", err)
	}

	got, err := svc.CurrentMood(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("CurrentMood() error = %v, want nil", err)
	}
	if got.Key != mood.KeyFeliz {
		t.Errorf("Key = %q, want %q", got.Key, mood.KeyFeliz)
	}
	if got.Note != "todo bien" {
		t.Errorf("Note = %q, want %q", got.Note, "todo bien")
	}
}

func TestSetMoodOverwritesPrevious(t *testing.T) {
	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := mood.NewService(pool)

	if _, err := svc.SetMood(t.Context(), u.ID, mood.KeyFeliz, "arriba"); err != nil {
		t.Fatalf("SetMood() #1 error = %v, want nil", err)
	}
	if _, err := svc.SetMood(t.Context(), u.ID, mood.KeyCansado, ""); err != nil {
		t.Fatalf("SetMood() #2 error = %v, want nil", err)
	}

	got, err := svc.CurrentMood(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("CurrentMood() error = %v, want nil", err)
	}
	if got.Key != mood.KeyCansado {
		t.Errorf("Key = %q, want %q — the second set should replace the first, not add a row",
			got.Key, mood.KeyCansado)
	}
	if got.Note != "" {
		t.Errorf("Note = %q, want empty", got.Note)
	}
}

func TestSetMoodRejectsInvalidKey(t *testing.T) {
	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := mood.NewService(pool)

	_, err := svc.SetMood(t.Context(), u.ID, mood.Key("euforico"), "")
	if !errors.Is(err, mood.ErrInvalidMood) {
		t.Fatalf("SetMood() error = %v, want ErrInvalidMood", err)
	}
}

func TestSetMoodRejectsInvalidNote(t *testing.T) {
	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := mood.NewService(pool)

	_, err := svc.SetMood(t.Context(), u.ID, mood.KeyFeliz, "línea uno\nlínea dos")
	if !errors.Is(err, mood.ErrInvalidNote) {
		t.Fatalf("SetMood() error = %v, want ErrInvalidNote", err)
	}
}

// TestSetMoodRecordsHistory guards the reason SetMood writes two tables in one
// transaction: every change must leave a trace behind, not just the current
// value.
func TestSetMoodRecordsHistory(t *testing.T) {
	pool := testdb.New(t)
	u := newAccount(t, pool)
	svc := mood.NewService(pool)

	if _, err := svc.SetMood(t.Context(), u.ID, mood.KeyFeliz, ""); err != nil {
		t.Fatalf("SetMood() #1 error = %v, want nil", err)
	}
	if _, err := svc.SetMood(t.Context(), u.ID, mood.KeyTriste, ""); err != nil {
		t.Fatalf("SetMood() #2 error = %v, want nil", err)
	}

	var count int
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*) FROM mood_history WHERE user_id = $1", u.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count mood_history: %v", err)
	}
	if count != 2 {
		t.Errorf("mood_history rows = %d, want 2", count)
	}
}
