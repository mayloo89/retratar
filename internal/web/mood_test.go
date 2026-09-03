package web_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestMoodSubmit_SetsAndShowsMood(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()

	resp := request(t, h, http.MethodPost, host, "/mood",
		strings.NewReader(url.Values{"mood_key": {"feliz"}, "note": {"todo bien"}}.Encode()), sessionCookie)
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /mood status = %d, want 303", resp.StatusCode)
	}

	home := request(t, h, http.MethodGet, host, "/", nil, sessionCookie)
	defer home.Body.Close() //nolint:errcheck // httptest body close cannot fail
	body, _ := io.ReadAll(home.Body)
	if !strings.Contains(string(body), "feliz") || !strings.Contains(string(body), "todo bien") {
		t.Errorf("home body = %q, want it to show the mood and note", body)
	}
}

func TestMoodSubmit_RejectsInvalidKey(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()

	resp := request(t, h, http.MethodPost, host, "/mood",
		strings.NewReader(url.Values{"mood_key": {"euforico"}}.Encode()), sessionCookie)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestMoodSubmit_RejectsLongNote(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()

	resp := request(t, h, http.MethodPost, host, "/mood",
		strings.NewReader(url.Values{"mood_key": {"feliz"}, "note": {strings.Repeat("a", 61)}}.Encode()), sessionCookie)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestMoodSubmit_RequiresSession(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	resp := request(t, h, http.MethodPost, host, "/mood",
		strings.NewReader(url.Values{"mood_key": {"feliz"}}.Encode()))
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (redirect to /login)", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/login" {
		t.Errorf("Location = %q, want %q", got, "/login")
	}
}
