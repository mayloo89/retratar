package guestbook_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/guestbook"
	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
)

// newAccount creates a real account with a claimed handle, the same way
// internal/mood/service_test.go does — both foreign keys on
// guestbook_entries need real rows in users.
func newAccount(t *testing.T, pool *pgxpool.Pool, email, handle string) user.User {
	t.Helper()

	svc := user.NewService(pool)
	raw, err := svc.RequestLogin(t.Context(), email)
	if err != nil {
		t.Fatalf("RequestLogin() error = %v, want nil", err)
	}
	u, err := svc.CompleteLogin(t.Context(), raw)
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v, want nil", err)
	}
	u, err = svc.ClaimHandle(t.Context(), u.ID, handle)
	if err != nil {
		t.Fatalf("ClaimHandle() error = %v, want nil", err)
	}
	return u
}

func countEntries(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM guestbook_entries`).Scan(&n); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	return n
}

func TestSignAndListNewestFirst(t *testing.T) {
	pool := testdb.New(t)
	page := newAccount(t, pool, "a@example.com", "a")
	b := newAccount(t, pool, "b@example.com", "b")
	c := newAccount(t, pool, "c@example.com", "c")
	svc := guestbook.NewService(pool)

	for _, s := range []struct {
		author user.User
		body   string
	}{{b, "primero"}, {c, "segundo"}, {b, "tercero"}} {
		if _, err := svc.Sign(t.Context(), page.ID, s.author.ID, s.body); err != nil {
			t.Fatalf("Sign(%q) error = %v, want nil", s.body, err)
		}
	}

	got, err := svc.Visible(t.Context(), page.ID, guestbook.PageLimit)
	if err != nil {
		t.Fatalf("Visible() error = %v, want nil", err)
	}
	want := []struct{ body, handle string }{{"tercero", "b"}, {"segundo", "c"}, {"primero", "b"}}
	if len(got) != len(want) {
		t.Fatalf("Visible() returned %d entries, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Body != w.body || got[i].AuthorHandle != w.handle {
			t.Errorf("entry %d = (%q, %q), want (%q, %q)", i, got[i].Body, got[i].AuthorHandle, w.body, w.handle)
		}
	}
}

func TestSignRejectsOwnPage(t *testing.T) {
	pool := testdb.New(t)
	a := newAccount(t, pool, "a@example.com", "a")
	svc := guestbook.NewService(pool)

	_, err := svc.Sign(t.Context(), a.ID, a.ID, "hola yo")
	if !errors.Is(err, guestbook.ErrOwnPage) {
		t.Fatalf("Sign() error = %v, want ErrOwnPage", err)
	}
	if n := countEntries(t, pool); n != 0 {
		t.Errorf("%d rows written, want 0", n)
	}
}

func TestVisibleExcludesHidden(t *testing.T) {
	pool := testdb.New(t)
	page := newAccount(t, pool, "a@example.com", "a")
	b := newAccount(t, pool, "b@example.com", "b")
	svc := guestbook.NewService(pool)

	hidden, err := svc.Sign(t.Context(), page.ID, b.ID, "oculto")
	if err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}
	if _, err = svc.Sign(t.Context(), page.ID, b.ID, "visible"); err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}
	if _, err = pool.Exec(t.Context(), `UPDATE guestbook_entries SET state = 'hidden' WHERE id = $1`, hidden.ID); err != nil {
		t.Fatalf("hide entry: %v", err)
	}

	got, err := svc.Visible(t.Context(), page.ID, guestbook.PageLimit)
	if err != nil {
		t.Fatalf("Visible() error = %v, want nil", err)
	}
	if len(got) != 1 || got[0].Body != "visible" {
		t.Errorf("Visible() = %+v, want only the visible entry", got)
	}
}

func TestVisibleRespectsLimit(t *testing.T) {
	pool := testdb.New(t)
	page := newAccount(t, pool, "a@example.com", "a")
	b := newAccount(t, pool, "b@example.com", "b")
	svc := guestbook.NewService(pool)

	if _, err := pool.Exec(t.Context(),
		`INSERT INTO guestbook_entries (page_user_id, author_user_id, body)
		 SELECT $1, $2, 'hola ' || g FROM generate_series(1, 25) g`, page.ID, b.ID); err != nil {
		t.Fatalf("insert entries: %v", err)
	}

	got, err := svc.Visible(t.Context(), page.ID, 20)
	if err != nil {
		t.Fatalf("Visible() error = %v, want nil", err)
	}
	if len(got) != 20 {
		t.Errorf("Visible() returned %d entries, want 20", len(got))
	}
}

func TestDeletingUserCascadesEntries(t *testing.T) {
	pool := testdb.New(t)
	a := newAccount(t, pool, "a@example.com", "a")
	b := newAccount(t, pool, "b@example.com", "b")
	c := newAccount(t, pool, "c@example.com", "c")
	svc := guestbook.NewService(pool)

	// b writes on a's page; c writes on b's page.
	if _, err := svc.Sign(t.Context(), a.ID, b.ID, "de b para a"); err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}
	if _, err := svc.Sign(t.Context(), b.ID, c.ID, "de c para b"); err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}

	// Deleting the author b removes what b wrote, and also b's own page.
	if _, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, b.ID); err != nil {
		t.Fatalf("delete author: %v", err)
	}
	if n := countEntries(t, pool); n != 0 {
		t.Errorf("%d entries after deleting author b, want 0", n)
	}

	// Deleting the page owner removes the entries on their page.
	if _, err := svc.Sign(t.Context(), a.ID, c.ID, "de c para a"); err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, a.ID); err != nil {
		t.Fatalf("delete page owner: %v", err)
	}
	if n := countEntries(t, pool); n != 0 {
		t.Errorf("%d entries after deleting page owner a, want 0", n)
	}
}

func TestOwnIncludesHidden(t *testing.T) {
	pool := testdb.New(t)
	page := newAccount(t, pool, "a@example.com", "a")
	b := newAccount(t, pool, "b@example.com", "b")
	svc := guestbook.NewService(pool)

	first, err := svc.Sign(t.Context(), page.ID, b.ID, "oculto")
	if err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}
	if _, err = svc.Sign(t.Context(), page.ID, b.ID, "visible"); err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}
	if err = svc.Hide(t.Context(), page.ID, first.ID); err != nil {
		t.Fatalf("Hide() error = %v, want nil", err)
	}

	got, err := svc.Own(t.Context(), page.ID, guestbook.OwnerLimit)
	if err != nil {
		t.Fatalf("Own() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("Own() returned %d entries, want 2", len(got))
	}
	// Newest first: "visible" was signed last.
	if got[0].Body != "visible" || got[0].Hidden {
		t.Errorf("Own()[0] = %+v, want the visible entry", got[0])
	}
	if got[1].Body != "oculto" || !got[1].Hidden || got[1].AuthorHandle != "b" {
		t.Errorf("Own()[1] = %+v, want the hidden entry by b", got[1])
	}

	visible, err := svc.Visible(t.Context(), page.ID, guestbook.PageLimit)
	if err != nil {
		t.Fatalf("Visible() error = %v, want nil", err)
	}
	if len(visible) != 1 || visible[0].Body != "visible" {
		t.Errorf("Visible() = %+v, want only the visible entry", visible)
	}
}

func TestHideAndShow(t *testing.T) {
	pool := testdb.New(t)
	page := newAccount(t, pool, "a@example.com", "a")
	b := newAccount(t, pool, "b@example.com", "b")
	svc := guestbook.NewService(pool)

	e, err := svc.Sign(t.Context(), page.ID, b.ID, "hola")
	if err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}

	if err = svc.Hide(t.Context(), page.ID, e.ID); err != nil {
		t.Fatalf("Hide() error = %v, want nil", err)
	}
	if got, _ := svc.Visible(t.Context(), page.ID, guestbook.PageLimit); len(got) != 0 {
		t.Errorf("Visible() after Hide = %+v, want none", got)
	}

	if err = svc.Show(t.Context(), page.ID, e.ID); err != nil {
		t.Fatalf("Show() error = %v, want nil", err)
	}
	if got, _ := svc.Visible(t.Context(), page.ID, guestbook.PageLimit); len(got) != 1 {
		t.Errorf("Visible() after Show = %+v, want the entry back", got)
	}
}

func TestHideOtherPagesEntryIsNotFound(t *testing.T) {
	pool := testdb.New(t)
	a := newAccount(t, pool, "a@example.com", "a")
	b := newAccount(t, pool, "b@example.com", "b")
	svc := guestbook.NewService(pool)

	e, err := svc.Sign(t.Context(), a.ID, b.ID, "hola")
	if err != nil {
		t.Fatalf("Sign() error = %v, want nil", err)
	}

	// b is not the owner of a's page.
	if err = svc.Hide(t.Context(), b.ID, e.ID); !errors.Is(err, guestbook.ErrEntryNotFound) {
		t.Errorf("Hide() by another owner error = %v, want ErrEntryNotFound", err)
	}
	if err = svc.Show(t.Context(), b.ID, e.ID); !errors.Is(err, guestbook.ErrEntryNotFound) {
		t.Errorf("Show() by another owner error = %v, want ErrEntryNotFound", err)
	}
	if got, _ := svc.Visible(t.Context(), a.ID, guestbook.PageLimit); len(got) != 1 {
		t.Errorf("Visible() = %+v, want the entry untouched", got)
	}

	// An ID that exists nowhere is reported the same way.
	if err = svc.Hide(t.Context(), a.ID, uuid.New()); !errors.Is(err, guestbook.ErrEntryNotFound) {
		t.Errorf("Hide() of unknown ID error = %v, want ErrEntryNotFound", err)
	}
}
