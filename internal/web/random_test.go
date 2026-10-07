package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/web"
)

const randomAppHost = "retratar.com.ar"

// claim signs address in and claims handle, returning the session cookie.
func claim(t *testing.T, srv *web.Server, sender *stubSender, address, handle string) *http.Cookie {
	t.Helper()
	h := srv.Handler()
	c := completeLogin(t, h, randomAppHost, sender, srv.Config.BaseURL(), address)
	request(t, h, http.MethodPost, randomAppHost, "/handle",
		strings.NewReader(url.Values{"handle": {handle}}.Encode()), c).Body.Close()
	return c
}

// randomLocation fetches /random and returns the response status and Location.
func randomLocation(t *testing.T, h http.Handler, cookies ...*http.Cookie) (int, string) {
	t.Helper()
	resp := request(t, h, http.MethodGet, randomAppHost, "/random", nil, cookies...)
	resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Location")
}

func TestRandom_RedirectsToAClaimedPage(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	claim(t, srv, sender, "a@example.com", "a")
	claim(t, srv, sender, "b@example.com", "b")
	completeLogin(t, h, randomAppHost, sender, srv.Config.BaseURL(), "c@example.com") // no handle

	for range 30 {
		status, loc := randomLocation(t, h)
		if status != http.StatusFound {
			t.Fatalf("status = %d, want 302", status)
		}
		if loc != "https://a.retrat.ar/" && loc != "https://b.retrat.ar/" {
			t.Fatalf("Location = %q, want a or b page", loc)
		}
	}
}

func TestRandom_ExcludesSignedInUser(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	a := claim(t, srv, sender, "a@example.com", "a")
	claim(t, srv, sender, "b@example.com", "b")

	for range 20 {
		status, loc := randomLocation(t, h, a)
		if status != http.StatusFound || loc != "https://b.retrat.ar/" {
			t.Fatalf("got %d %q, want 302 https://b.retrat.ar/", status, loc)
		}
	}
}

func TestRandom_NoEligiblePageRedirectsHome(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()

	if status, loc := randomLocation(t, h); status != http.StatusFound || loc != "/" {
		t.Fatalf("empty db: got %d %q, want 302 /", status, loc)
	}

	a := claim(t, srv, sender, "a@example.com", "a")
	if status, loc := randomLocation(t, h, a); status != http.StatusFound || loc != "/" {
		t.Fatalf("only self: got %d %q, want 302 /", status, loc)
	}
}

func TestRandom_NotCached(t *testing.T) {
	srv, _ := newLoginServer(t)
	resp := request(t, srv.Handler(), http.MethodGet, randomAppHost, "/random", nil)
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
}
