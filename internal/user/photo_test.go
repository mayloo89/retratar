package user_test

import (
	"errors"
	"sync"
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

	old, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, day1)
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
	if _, err = svc.SetPhoto(t.Context(), u.ID, keyB, 10, 10, day1); !errors.Is(err, user.ErrPhotoNotAllowed) {
		t.Fatalf("second SetPhoto() error = %v, want ErrPhotoNotAllowed", err)
	}
	if got, _ = svc.GetByID(t.Context(), u.ID); got.PhotoKey != keyA {
		t.Errorf("PhotoKey after refused upload = %q, want %q", got.PhotoKey, keyA)
	}

	// A later day start: allowed, and it returns the first key.
	old, err = svc.SetPhoto(t.Context(), u.ID, keyB, 10, 10, day1.Add(24*time.Hour))
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

	if _, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, day1); !errors.Is(err, user.ErrPhotoNotAllowed) {
		t.Fatalf("SetPhoto() error = %v, want ErrPhotoNotAllowed", err)
	}
	if got, _ := svc.GetByID(t.Context(), u.ID); got.PhotoKey != "" {
		t.Errorf("PhotoKey = %q, want empty", got.PhotoKey)
	}
}

func TestSetPhoto_ConcurrentUploadsOneWins(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := photoUser(t, pool, svc, true)

	keys := []string{keyA, keyB, keyC}
	errs := make([]error, len(keys))
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = svc.SetPhoto(t.Context(), u.ID, k, 10, 10, day1)
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, user.ErrPhotoNotAllowed):
			t.Errorf("SetPhoto() error = %v, want nil or ErrPhotoNotAllowed", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d concurrent uploads succeeded, want exactly 1", wins)
	}
}

func TestRemovePhoto_KeepsUploadTime(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := photoUser(t, pool, svc, true)

	if _, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, day1); err != nil {
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
	if _, err = svc.SetPhoto(t.Context(), u.ID, keyB, 10, 10, day1); !errors.Is(err, user.ErrPhotoNotAllowed) {
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
	if _, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, day1); err != nil {
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
	if _, err := svc.SetPhoto(t.Context(), u.ID, keyA, 40, 20, day1); err != nil {
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
