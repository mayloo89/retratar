package user_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
)

const (
	keyA = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	keyB = "bbbbbbbbbbbbbbbbbbbbbbbbbb"
	keyC = "cccccccccccccccccccccccccc"
)

var day1 = time.Date(2026, 10, 20, 0, 0, 0, 0, time.FixedZone("ART", -3*3600))

// photoUser creates an account, allowlisted when allow is true.
func photoUser(t *testing.T, pool *pgxpool.Pool, svc *user.Service, allow bool) user.User {
	t.Helper()
	raw, err := svc.RequestLogin(t.Context(), "ana@example.com")
	if err != nil {
		t.Fatalf("RequestLogin() error = %v", err)
	}
	u, err := svc.CompleteLogin(t.Context(), raw)
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v", err)
	}
	if allow {
		if _, err := pool.Exec(t.Context(), `UPDATE users SET can_upload_photos = true WHERE id = $1`, u.ID); err != nil {
			t.Fatalf("allowlist: %v", err)
		}
	}
	return u
}

func TestSetPhoto(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := photoUser(t, pool, svc, true)

	old, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, day1, day1)
	if err != nil {
		t.Fatalf("SetPhoto() error = %v", err)
	}
	if old != "" {
		t.Errorf("previous key = %q, want empty", old)
	}

	got, err := svc.GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PhotoKey != keyA || got.PhotoWidth != 40 || got.PhotoHeight != 20 {
		t.Errorf("stored photo = %q %dx%d, want %q 40x20", got.PhotoKey, got.PhotoWidth, got.PhotoHeight, keyA)
	}
	if got.PhotoUploadedAt.IsZero() || !got.CanUploadPhotos {
		t.Errorf("PhotoUploadedAt = %v, CanUploadPhotos = %v, want set and true", got.PhotoUploadedAt, got.CanUploadPhotos)
	}

	// Same day: refused, and nothing changes.
	if _, err = svc.SetPhoto(t.Context(), u.ID, keyB, 10, 10, day1, day1); !errors.Is(err, user.ErrPhotoNotAllowed) {
		t.Fatalf("second SetPhoto() error = %v, want ErrPhotoNotAllowed", err)
	}
	if got, _ = svc.GetByID(t.Context(), u.ID); got.PhotoKey != keyA {
		t.Errorf("PhotoKey after refused upload = %q, want %q", got.PhotoKey, keyA)
	}

	// A later day start: allowed, and it returns the first key.
	old, err = svc.SetPhoto(t.Context(), u.ID, keyB, 10, 10, day1.Add(24*time.Hour), day1.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("SetPhoto() next day error = %v", err)
	}
	if old != keyA {
		t.Errorf("previous key = %q, want %q", old, keyA)
	}
}

func TestSetPhoto_NotAllowlisted(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := photoUser(t, pool, svc, false)

	if _, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, day1, day1); !errors.Is(err, user.ErrPhotoNotAllowed) {
		t.Fatalf("SetPhoto() error = %v, want ErrPhotoNotAllowed", err)
	}
	if got, _ := svc.GetByID(t.Context(), u.ID); got.PhotoKey != "" {
		t.Errorf("PhotoKey = %q, want empty", got.PhotoKey)
	}
}

// TestSetPhoto_UsesCallerClock fails if the query stamps the upload with the
// database's clock instead of the time the caller passed.
func TestSetPhoto_UsesCallerClock(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := photoUser(t, pool, svc, true)

	longAgo := time.Date(2020, 1, 1, 9, 30, 0, 0, time.UTC)
	if _, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, longAgo.Add(-time.Hour), longAgo); err != nil {
		t.Fatalf("SetPhoto() error = %v", err)
	}

	var got time.Time
	if err := pool.QueryRow(t.Context(), `SELECT photo_uploaded_at FROM users WHERE id = $1`, u.ID).Scan(&got); err != nil {
		t.Fatalf("read photo_uploaded_at: %v", err)
	}
	if !got.Equal(longAgo) {
		t.Errorf("photo_uploaded_at = %v, want %v", got, longAgo)
	}
}

// TestSetPhoto_UploadCommittedWhileWaitingIsRefused forces the interleaving: a
// first upload holds the row lock, a second SetPhoto is started and is
// confirmed to be blocked on that lock, and only then does the first commit.
// It shows that only one upload wins. It does not show that FOR UPDATE is
// needed for that: Postgres re-checks an UPDATE's WHERE on the updated row
// anyway. FOR UPDATE is what makes the returned old key the latest one.
func TestSetPhoto_UploadCommittedWhileWaitingIsRefused(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := photoUser(t, pool, svc, true)

	// Transaction 1: lock the row and write what a committed first upload
	// would have written.
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	// Tests are isolated by schema inside one database, so another parallel
	// test can also be waiting on a lock. Wait for a backend blocked by this
	// transaction's own pid, and nothing else.
	var holderPID int
	if err = tx.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(),
		`UPDATE users SET photo_key = $2, photo_width = 10, photo_height = 10, photo_uploaded_at = $3 WHERE id = $1`,
		u.ID, keyA, day1); err != nil {
		t.Fatal(err)
	}

	type result struct {
		old string
		err error
	}
	done := make(chan result, 1)
	go func() {
		old, setErr := svc.SetPhoto(t.Context(), u.ID, keyB, 10, 10, day1, day1)
		done <- result{old, setErr}
	}()

	// Wait until SetPhoto is really blocked on the row lock.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		err = pool.QueryRow(t.Context(),
			`SELECT count(*) FROM pg_stat_activity WHERE $1 = ANY (pg_blocking_pids(pid))`, holderPID).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case r := <-done:
			t.Fatalf("SetPhoto returned %q, %v before the lock was released", r.old, r.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("SetPhoto never blocked on the row lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if !errors.Is(r.err, user.ErrPhotoNotAllowed) {
		t.Fatalf("SetPhoto() = %q, %v, want ErrPhotoNotAllowed", r.old, r.err)
	}
	got, err := svc.GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PhotoKey != keyA {
		t.Errorf("PhotoKey = %q, want the first upload's %q", got.PhotoKey, keyA)
	}
}

func TestRemovePhoto_KeepsUploadTime(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := photoUser(t, pool, svc, true)

	if _, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, day1, day1); err != nil {
		t.Fatal(err)
	}
	old, err := svc.RemovePhoto(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("RemovePhoto() error = %v", err)
	}
	if old != keyA {
		t.Errorf("RemovePhoto() key = %q, want %q", old, keyA)
	}

	got, err := svc.GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PhotoKey != "" || got.PhotoWidth != 0 || got.PhotoHeight != 0 {
		t.Errorf("photo after remove = %q %dx%d, want none", got.PhotoKey, got.PhotoWidth, got.PhotoHeight)
	}
	if got.PhotoUploadedAt.IsZero() {
		t.Error("PhotoUploadedAt was cleared by RemovePhoto, want it kept")
	}
	if _, err = svc.SetPhoto(t.Context(), u.ID, keyB, 10, 10, day1, day1); !errors.Is(err, user.ErrPhotoNotAllowed) {
		t.Errorf("SetPhoto() after remove error = %v, want ErrPhotoNotAllowed", err)
	}

	// Removing with no photo is a no-op, not an error.
	old, err = svc.RemovePhoto(t.Context(), u.ID)
	if err != nil || old != "" {
		t.Errorf("second RemovePhoto() = %q, %v, want empty and nil", old, err)
	}
}

func TestRemovePhoto_UnknownUser(t *testing.T) {
	t.Parallel()
	svc := user.NewService(testdb.New(t))
	if _, err := svc.RemovePhoto(t.Context(), uuid.New()); !errors.Is(err, user.ErrUserNotFound) {
		t.Errorf("RemovePhoto() error = %v, want ErrUserNotFound", err)
	}
}

func TestByPhotoKey(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := photoUser(t, pool, svc, true)
	if _, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, day1, day1); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ByPhotoKey(t.Context(), keyA)
	if err != nil || got.ID != u.ID {
		t.Fatalf("ByPhotoKey() = %v, %v, want the owner", got.ID, err)
	}
	if _, err := svc.ByPhotoKey(t.Context(), keyB); !errors.Is(err, user.ErrUserNotFound) {
		t.Errorf("ByPhotoKey(unknown) error = %v, want ErrUserNotFound", err)
	}
	if _, err := svc.RemovePhoto(t.Context(), u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ByPhotoKey(t.Context(), keyA); !errors.Is(err, user.ErrUserNotFound) {
		t.Errorf("ByPhotoKey(removed) error = %v, want ErrUserNotFound", err)
	}
}

func TestPhotoColumnsConsistent(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := photoUser(t, pool, svc, true)

	for name, stmt := range map[string]string{
		"key without size": `UPDATE users SET photo_key = 'aaaaaaaaaaaaaaaaaaaaaaaaaa' WHERE id = $1`,
		"size without key": `UPDATE users SET photo_width = 10, photo_height = 10 WHERE id = $1`,
		"malformed key":    `UPDATE users SET photo_key = 'not-a-key', photo_width = 10, photo_height = 10 WHERE id = $1`,
		"zero width":       `UPDATE users SET photo_key = 'aaaaaaaaaaaaaaaaaaaaaaaaaa', photo_width = 0, photo_height = 10 WHERE id = $1`,
		"uppercase key":    `UPDATE users SET photo_key = 'AAAAAAAAAAAAAAAAAAAAAAAAAA', photo_width = 10, photo_height = 10 WHERE id = $1`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := pool.Exec(t.Context(), stmt, u.ID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("Exec() error = %v, want a CHECK violation (23514)", err)
			}
		})
	}
}

func TestPhotoKeyIsUnique(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := photoUser(t, pool, svc, true)
	if _, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, day1, day1); err != nil {
		t.Fatal(err)
	}

	raw, err := svc.RequestLogin(t.Context(), "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := svc.CompleteLogin(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(t.Context(),
		`UPDATE users SET photo_key = $2, photo_width = 1, photo_height = 1 WHERE id = $1`, bob.ID, keyA)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("Exec() error = %v, want a unique violation (23505)", err)
	}
}

func TestUploadedPhotoOn(t *testing.T) {
	t.Parallel()
	art := time.FixedZone("ART", -3*3600)
	uploaded := time.Date(2026, 10, 20, 23, 30, 0, 0, art) // 02:30 UTC the next day

	tests := []struct {
		name string
		u    user.User
		day  time.Time
		want bool
	}{
		{"never uploaded", user.User{}, time.Date(2026, 10, 20, 12, 0, 0, 0, art), false},
		{"same day", user.User{PhotoUploadedAt: uploaded}, time.Date(2026, 10, 20, 8, 0, 0, 0, art), true},
		{"same day, uploaded in UTC", user.User{PhotoUploadedAt: uploaded.UTC()}, time.Date(2026, 10, 20, 8, 0, 0, 0, art), true},
		{"next day", user.User{PhotoUploadedAt: uploaded}, time.Date(2026, 10, 21, 0, 30, 0, 0, art), false},
		{"previous day", user.User{PhotoUploadedAt: uploaded}, time.Date(2026, 10, 19, 23, 59, 0, 0, art), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.u.UploadedPhotoOn(tt.day); got != tt.want {
				t.Errorf("UploadedPhotoOn() = %v, want %v", got, tt.want)
			}
		})
	}
}
