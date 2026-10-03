package web_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/web"
)

// requestWithBodyHeaders is [request] plus extra request headers, which the
// origin check reads. Host is set separately by request's host argument.
func requestWithBodyHeaders(t *testing.T, h http.Handler, headers map[string]string, method, host, path string, body io.Reader, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Host = host
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

// signedInWithHandle logs ana in, claims a handle, and returns her session
// cookie and user ID.
func signedInWithHandle(t *testing.T, srv *web.Server, h http.Handler, sender *stubSender, host string) (*http.Cookie, uuid.UUID) {
	t.Helper()
	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()
	id, err := srv.Sessions.Lookup(t.Context(), sessionCookie.Value)
	if err != nil {
		t.Fatalf("Sessions.Lookup() error = %v", err)
	}
	return sessionCookie, id
}

func TestCrossOriginProtection_RejectsCrossSitePOST(t *testing.T) {
	// same-site is the sibling-subdomain case that SameSite=Lax lets through.
	for _, site := range []string{"cross-site", "same-site"} {
		t.Run(site, func(t *testing.T) {
			srv, sender := newLoginServer(t)
			h := srv.Handler()
			host := "retratar.com.ar"
			sessionCookie, id := signedInWithHandle(t, srv, h, sender, host)
			hdr := map[string]string{"Sec-Fetch-Site": site}

			post := func(path string, form url.Values, cookies ...*http.Cookie) {
				t.Helper()
				resp := requestWithBodyHeaders(t, h, hdr, http.MethodPost, host, path, strings.NewReader(form.Encode()), cookies...)
				defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("POST %s status = %d, want 403", path, resp.StatusCode)
				}
				if body, _ := io.ReadAll(resp.Body); string(body) != "forbidden\n" {
					t.Errorf("POST %s body = %q, want %q", path, body, "forbidden\n")
				}
			}

			post("/mood", url.Values{"mood_key": {"feliz"}}, sessionCookie)
			if _, err := srv.Moods.CurrentMood(t.Context(), id); !errors.Is(err, mood.ErrNoMood) {
				t.Errorf("CurrentMood() error = %v, want ErrNoMood: a refused POST /mood must not set a mood", err)
			}

			post("/handle", url.Values{"handle": {"other"}}, sessionCookie)
			if u, err := srv.Users.GetByID(t.Context(), id); err != nil || u.Handle != "ana" {
				t.Errorf("user = %+v, err = %v, want handle ana unchanged", u, err)
			}

			post("/logout", url.Values{}, sessionCookie)
			if _, err := srv.Sessions.Lookup(t.Context(), sessionCookie.Value); err != nil {
				t.Errorf("Sessions.Lookup() error = %v, want the session still valid after a refused logout", err)
			}

			sender.mu.Lock()
			sender.body = ""
			sender.mu.Unlock()
			post("/login", url.Values{"email": {"bob@example.com"}})
			sender.mu.Lock()
			sent := sender.body
			sender.mu.Unlock()
			if sent != "" {
				t.Errorf("a refused POST /login sent mail: %q", sent)
			}
		})
	}
}

func TestCrossOriginProtection_RejectsForeignOriginWithoutFetchMetadata(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"
	sessionCookie, id := signedInWithHandle(t, srv, h, sender, host)

	resp := requestWithBodyHeaders(t, h, map[string]string{"Origin": "https://evil.example"},
		http.MethodPost, host, "/mood",
		strings.NewReader(url.Values{"mood_key": {"feliz"}}.Encode()), sessionCookie)
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if _, err := srv.Moods.CurrentMood(t.Context(), id); !errors.Is(err, mood.ErrNoMood) {
		t.Errorf("CurrentMood() error = %v, want ErrNoMood", err)
	}
}

func TestCrossOriginProtection_AllowsSameOrigin(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
	}{
		{"same-origin fetch metadata", map[string]string{"Sec-Fetch-Site": "same-origin"}},
		{"matching Origin", map[string]string{"Origin": "https://retratar.com.ar"}},
		// Non-browser clients send neither header and are not a CSRF concern.
		{"no headers", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, sender := newLoginServer(t)
			h := srv.Handler()
			host := "retratar.com.ar"
			sessionCookie, _ := signedInWithHandle(t, srv, h, sender, host)

			resp := requestWithBodyHeaders(t, h, tt.headers, http.MethodPost, host, "/mood",
				strings.NewReader(url.Values{"mood_key": {"feliz"}}.Encode()), sessionCookie)
			resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
			if resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", resp.StatusCode)
			}
		})
	}
}

func TestCrossOriginProtection_PagesSurfaceUnaffected(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	signedInWithHandle(t, srv, h, sender, "retratar.com.ar")

	resp := requestWithBodyHeaders(t, h, map[string]string{"Sec-Fetch-Site": "cross-site"},
		http.MethodGet, "ana.retrat.ar", "/", nil)
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestBodyLimit_RejectsOversizedPOST(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	big := url.Values{"email": {"a@example.com"}, "pad": {strings.Repeat("x", 5<<10)}}.Encode()
	resp := request(t, h, http.MethodPost, host, "/login", strings.NewReader(big))
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("5 KiB body status = %d, want 413", resp.StatusCode)
	}

	small := url.Values{"email": {"a@example.com"}, "pad": {strings.Repeat("x", 1<<10)}}.Encode()
	resp = request(t, h, http.MethodPost, host, "/login", strings.NewReader(small))
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		t.Errorf("1 KiB body status = %d, want it accepted", resp.StatusCode)
	}
}
