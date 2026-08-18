package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mayloo89/retratar/internal/web"
)

// TestSessionCookieIsHostOnly asserts the attributes that keep the session
// cookie unreachable from a user page.
//
// The __Host- prefix is enforced by the browser: it drops the cookie unless it
// is Secure, has Path=/, and carries no Domain. No Domain means host-only, so
// no subdomain can read it and no subdomain can overwrite it. Any change that
// breaks one of these assertions reopens session fixation.
func TestSessionCookieIsHostOnly(t *testing.T) {
	rec := httptest.NewRecorder()

	web.SetSessionCookie(rec, "token-value", time.Hour)

	raw := rec.Header().Get("Set-Cookie")
	if raw == "" {
		t.Fatal("SetSessionCookie wrote no Set-Cookie header")
	}
	if !strings.HasPrefix(raw, "__Host-") {
		t.Errorf("cookie name lacks the __Host- prefix: %s", raw)
	}
	// The Domain attribute would widen the cookie to every subdomain and is
	// also rejected outright by browsers under the __Host- prefix.
	if strings.Contains(strings.ToLower(raw), "domain=") {
		t.Errorf("cookie carries a Domain attribute: %s", raw)
	}

	resp := http.Response{Header: rec.Header()}
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]

	if c.Name != web.SessionCookieName {
		t.Errorf("Name = %q, want %q", c.Name, web.SessionCookieName)
	}
	if c.Domain != "" {
		t.Errorf("Domain = %q, want empty", c.Domain)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want %q", c.Path, "/")
	}
	if !c.Secure {
		t.Error("Secure = false, want true")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.MaxAge != int(time.Hour.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(time.Hour.Seconds()))
	}
}

// TestClearSessionCookieMatchesSetAttributes checks that logout actually logs
// out. A browser replaces a cookie only when name, path and domain match, so
// an expiry written with different attributes leaves the session alive.
func TestClearSessionCookieMatchesSetAttributes(t *testing.T) {
	set := httptest.NewRecorder()
	web.SetSessionCookie(set, "token-value", time.Hour)
	cleared := httptest.NewRecorder()
	web.ClearSessionCookie(cleared)

	setCookie := (&http.Response{Header: set.Header()}).Cookies()[0]
	clearCookie := (&http.Response{Header: cleared.Header()}).Cookies()[0]

	if setCookie.Name != clearCookie.Name {
		t.Errorf("Name: set %q, clear %q", setCookie.Name, clearCookie.Name)
	}
	if setCookie.Path != clearCookie.Path {
		t.Errorf("Path: set %q, clear %q", setCookie.Path, clearCookie.Path)
	}
	if setCookie.Domain != clearCookie.Domain {
		t.Errorf("Domain: set %q, clear %q", setCookie.Domain, clearCookie.Domain)
	}
	if setCookie.Secure != clearCookie.Secure {
		t.Errorf("Secure: set %v, clear %v", setCookie.Secure, clearCookie.Secure)
	}
	if clearCookie.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative so the browser deletes it", clearCookie.MaxAge)
	}
	if clearCookie.Value != "" {
		t.Errorf("Value = %q, want empty", clearCookie.Value)
	}
}
