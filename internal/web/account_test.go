package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	msgAdult    = "Para usar retratar tenés que tener 18 años o más y aceptar los términos."
	msgTaken    = "Esa dirección ya está tomada. Probá otra."
	msgConfirm  = "Marcá la casilla para confirmar."
	deleteForm  = "application/x-www-form-urlencoded"
	deletedPath = "/account/deleted"
)

// form posts values to path as c.
func (e *photoEnv) form(t *testing.T, c *http.Cookie, path string, v url.Values, hdr ...string) reply {
	t.Helper()
	return e.post(t, c, path, deleteForm, strings.NewReader(v.Encode()), hdr...)
}

// count runs a count query on the test schema.
func (e *photoEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// claimAs signs in address and posts the claim form for handle, with extra
// fields merged in. It returns the session cookie and the claim's reply.
func (e *photoEnv) claimAs(t *testing.T, address, handle string, extra url.Values) (*http.Cookie, reply) {
	t.Helper()
	c := completeLogin(t, e.h, appHost, e.send, e.srv.Config.BaseURL(), address)
	v := url.Values{"handle": {handle}}
	for k, vals := range extra {
		v[k] = vals
	}
	return c, e.form(t, c, "/handle", v)
}

func TestClaim_RequiresAdult(t *testing.T) {
	e := newPhotoEnv(t)

	for _, tt := range []struct {
		slug  string
		extra url.Values
	}{
		{"missing", nil},
		{"on", url.Values{"adult": {"on"}}},
		{"empty", url.Values{"adult": {""}}},
	} {
		t.Run(tt.slug, func(t *testing.T) {
			address, handle := tt.slug+"@example.com", "ana-"+tt.slug
			c, r := e.claimAs(t, address, handle, tt.extra)
			wantBodyHas(t, r, http.StatusUnprocessableEntity, msgAdult)
			if !strings.Contains(string(r.Body), `value="`+handle+`"`) {
				t.Errorf("the typed handle was not kept; body: %.600s", r.Body)
			}
			wantBodyHas(t, e.status(t, appHost, "/handle", c), http.StatusOK, "Elegí tu dirección")
			if n := e.count(t, `SELECT count(*) FROM users WHERE email = $1 AND handle IS NOT NULL`, address); n != 0 {
				t.Errorf("handle was claimed without the checkbox")
			}
			if n := e.count(t, `SELECT count(*) FROM users WHERE email = $1 AND terms_accepted_at IS NOT NULL`, address); n != 0 {
				t.Errorf("terms_accepted_at was set without the checkbox")
			}
		})
	}
}

func TestClaim_StoresTermsAcceptance(t *testing.T) {
	e := newPhotoEnv(t)
	at := time.Date(2026, 10, 21, 9, 30, 15, 123456000, art)
	e.clock.Set(at)

	_, r := e.claimAs(t, "ana@example.com", "ana", url.Values{"adult": {"yes"}})
	wantStatus(t, r, http.StatusSeeOther)

	var got time.Time
	if err := e.pool.QueryRow(t.Context(), `SELECT terms_accepted_at FROM users WHERE handle = 'ana'`).Scan(&got); err != nil {
		t.Fatalf("terms_accepted_at: %v", err)
	}
	if !got.Equal(at) {
		t.Errorf("terms_accepted_at = %s, want %s", got, at)
	}
}

// seedAccount is the account the delete tests tear down: a handle, a mood with
// history, a photo, an entry in its book signed by bob, and an entry it wrote
// in bob's book.
type seedAccount struct {
	ana, bob *http.Cookie
	photo    string
}

func seedAnaAndBob(t *testing.T, e *photoEnv) seedAccount {
	t.Helper()
	s := seedAccount{ana: e.signUp(t, "ana@example.com", "ana", true), bob: e.signUp(t, "bob@example.com", "bob", false)}

	wantStatus(t, e.form(t, s.ana, "/mood", url.Values{"mood_key": {"feliz"}, "note": {"bien"}}), http.StatusSeeOther)
	wantStatus(t, e.form(t, s.ana, "/mood", url.Values{"mood_key": {"triste"}}), http.StatusSeeOther)
	wantStatus(t, e.upload(t, s.ana, testJPEG(t, 40, 20)), http.StatusSeeOther)
	wantStatus(t, e.form(t, s.bob, "/sign/ana", url.Values{"body": {"hola ana, de bob"}}), http.StatusSeeOther)
	wantStatus(t, e.form(t, s.ana, "/sign/bob", url.Values{"body": {"hola bob, de ana"}}), http.StatusSeeOther)

	files := e.files(t)
	if len(files) != 1 {
		t.Fatalf("photo store has %v, want exactly ana's photo", files)
	}
	s.photo = files[0]
	return s
}

func TestDeleteAccount_RemovesEverything(t *testing.T) {
	e := newPhotoEnv(t)
	s := seedAnaAndBob(t, e)

	// Ana also has a published style, which must go with her.
	e.publish(t, s.ana, "triste", with("c_bg", "#ff00cc", "f_head", "impact"))

	// Prove the seed is not empty, or "gone" below would prove nothing.
	for q, want := range map[string]int{
		`SELECT count(*) FROM page_styles`:                                  1,
		`SELECT count(*) FROM moods`:                                        1, // ana's
		`SELECT count(*) FROM mood_history`:                                 2,
		`SELECT count(*) FROM guestbook_entries`:                            2,
		`SELECT count(*) FROM login_tokens WHERE email = 'ana@example.com'`: 1,
	} {
		if got := e.count(t, q); got != want {
			t.Fatalf("before: %s = %d, want %d", q, got, want)
		}
	}

	r := e.form(t, s.ana, "/account/delete", url.Values{"confirm": {"yes"}})
	wantStatus(t, r, http.StatusSeeOther)
	if loc := r.Header.Get("Location"); loc != deletedPath {
		t.Errorf("Location = %q, want %q", loc, deletedPath)
	}
	var cleared bool
	for _, c := range r.Cookies {
		if c.Name == "__Host-session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("the session cookie was not cleared; cookies: %v", r.Cookies)
	}

	for q, want := range map[string]int{
		`SELECT count(*) FROM users WHERE email = 'ana@example.com'`:                                         0,
		`SELECT count(*) FROM sessions s JOIN users u ON u.id = s.user_id WHERE u.email = 'ana@example.com'`: 0,
		`SELECT count(*) FROM page_styles`:                                                                   0,
		`SELECT count(*) FROM moods`:                                                                         0,
		`SELECT count(*) FROM mood_history`:                                                                  0,
		`SELECT count(*) FROM login_tokens WHERE lower(email) = 'ana@example.com'`:                           0,
		`SELECT count(*) FROM guestbook_entries`:                                                             0,
		`SELECT count(*) FROM users WHERE email = 'bob@example.com'`:                                         1,
		`SELECT count(*) FROM sessions`:                                                                      1, // bob's
	} {
		if got := e.count(t, q); got != want {
			t.Errorf("after: %s = %d, want %d", q, got, want)
		}
	}
	if files := e.files(t); len(files) != 0 {
		t.Errorf("photo store has %v, want the photo gone from disk", files)
	}

	// The old cookie is a dead session.
	home := e.status(t, appHost, "/", s.ana)
	wantStatus(t, home, http.StatusSeeOther)
	if loc := home.Header.Get("Location"); loc != "/login" {
		t.Errorf("GET / Location = %q, want /login", loc)
	}
	// And bob is untouched.
	wantBodyHas(t, e.status(t, appHost, "/", s.bob), http.StatusOK, "https://bob.retrat.ar")
}

func TestDeleteAccount_RequiresConfirm(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	for name, v := range map[string]url.Values{"missing": {}, "not exactly": {"confirm": {"on"}}} {
		t.Run(name, func(t *testing.T) {
			wantBodyHas(t, e.form(t, c, "/account/delete", v), http.StatusUnprocessableEntity, msgConfirm)
			if n := e.count(t, `SELECT count(*) FROM users WHERE handle = 'ana'`); n != 1 {
				t.Errorf("the account was deleted without confirmation")
			}
			if n := e.count(t, `SELECT count(*) FROM deleted_handles`); n != 0 {
				t.Errorf("a handle was reserved without confirmation")
			}
		})
	}
}

func TestDeleteAccount_Guards(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)
	yes := url.Values{"confirm": {"yes"}}

	t.Run("signed out is sent to login", func(t *testing.T) {
		for _, r := range []reply{
			e.form(t, nil, "/account/delete", yes),
			e.status(t, appHost, "/account/delete"),
		} {
			wantStatus(t, r, http.StatusSeeOther)
			if loc := r.Header.Get("Location"); loc != "/login" {
				t.Errorf("Location = %q, want /login", loc)
			}
		}
	})

	t.Run("cross-origin is refused", func(t *testing.T) {
		wantStatus(t, e.form(t, c, "/account/delete", yes, "Origin", "https://evil.example"), http.StatusForbidden)
		wantStatus(t, e.form(t, c, "/account/delete", yes, "Sec-Fetch-Site", "cross-site"), http.StatusForbidden)
		if n := e.count(t, `SELECT count(*) FROM users WHERE handle = 'ana'`); n != 1 {
			t.Errorf("a cross-origin POST deleted the account")
		}
	})

	t.Run("the confirmation page is a plain GET that changes nothing", func(t *testing.T) {
		r := e.status(t, appHost, "/account/delete", c)
		wantBodyHas(t, r, http.StatusOK, "Borrar tu cuenta")
		for _, want := range []string{"ana.retrat.ar", "90 días", `name="confirm"`, "No, volver"} {
			if !strings.Contains(string(r.Body), want) {
				t.Errorf("confirmation page lacks %q", want)
			}
		}
		if n := e.count(t, `SELECT count(*) FROM users WHERE handle = 'ana'`); n != 1 {
			t.Errorf("GET deleted the account")
		}
	})

	t.Run("the goodbye page needs no session", func(t *testing.T) {
		wantBodyHas(t, e.status(t, appHost, deletedPath), http.StatusOK, "Tu cuenta y todo lo que tenía se borraron.")
	})
}

func TestDeleteAccount_NoHandle(t *testing.T) {
	e := newPhotoEnv(t)
	c := completeLogin(t, e.h, appHost, e.send, e.srv.Config.BaseURL(), "ana@example.com")

	page := e.status(t, appHost, "/account/delete", c)
	wantBodyHas(t, page, http.StatusOK, "Borrar tu cuenta")
	if strings.Contains(string(page.Body), "90 días") {
		t.Errorf("an account with no handle was told its handle is reserved")
	}

	r := e.form(t, c, "/account/delete", url.Values{"confirm": {"yes"}})
	wantStatus(t, r, http.StatusSeeOther)
	if n := e.count(t, `SELECT count(*) FROM users`); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
	if n := e.count(t, `SELECT count(*) FROM deleted_handles`); n != 0 {
		t.Errorf("deleted_handles = %d, want 0 for an account with no handle", n)
	}
}

func TestDeletedHandle_Reserved(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "sebas@example.com", "sebas", false)
	wantStatus(t, e.form(t, c, "/account/delete", url.Values{"confirm": {"yes"}}), http.StatusSeeOther)

	var stored string
	if err := e.pool.QueryRow(t.Context(), `SELECT handle FROM deleted_handles`).Scan(&stored); err != nil || stored != "sebas" {
		t.Fatalf("deleted_handles = %q, %v; want %q", stored, err, "sebas")
	}

	yes := url.Values{"adult": {"yes"}}
	_, r := e.claimAs(t, "otra@example.com", "sebas", yes)
	wantBodyHas(t, r, http.StatusConflict, msgTaken)

	// Upper case never reaches the reservation: the handle rule refuses it
	// first, so it cannot be claimed either.
	c2, r := e.claimAs(t, "otra2@example.com", "SEBAS", yes)
	wantStatus(t, r, http.StatusUnprocessableEntity)
	if n := e.count(t, `SELECT count(*) FROM users WHERE handle IS NOT NULL`); n != 0 {
		t.Fatalf("a reserved handle was claimed")
	}

	// 91 days later the reservation is over.
	e.clock.Set(e.clock.Now().Add(91 * 24 * time.Hour))
	wantStatus(t, e.form(t, c2, "/handle", url.Values{"handle": {"sebas"}, "adult": {"yes"}}), http.StatusSeeOther)
	if n := e.count(t, `SELECT count(*) FROM users WHERE handle = 'sebas'`); n != 1 {
		t.Errorf("the handle was not released after 90 days")
	}
}

func TestPurge_DeletedHandles(t *testing.T) {
	e := newPhotoEnv(t)
	if _, err := e.pool.Exec(t.Context(), `
		INSERT INTO deleted_handles (handle, deleted_at) VALUES
		  ('viejo', now() - interval '91 days'),
		  ('joven', now() - interval '89 days')`); err != nil {
		t.Fatal(err)
	}

	n, err := e.srv.Users.PurgeDeletedHandles(t.Context(), 90*24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("PurgeDeletedHandles() = %d, %v; want 1, nil", n, err)
	}
	var left string
	if err := e.pool.QueryRow(t.Context(), `SELECT handle FROM deleted_handles`).Scan(&left); err != nil || left != "joven" {
		t.Errorf("remaining = %q, %v; want only %q", left, err, "joven")
	}
}
