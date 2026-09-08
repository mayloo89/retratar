package web_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestClaimHandleFlow_ShowsFormThenClaims(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")

	form := request(t, h, http.MethodGet, host, "/handle", nil, sessionCookie)
	defer form.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if form.StatusCode != http.StatusOK {
		t.Fatalf("GET /handle status = %d, want 200", form.StatusCode)
	}
	body, _ := io.ReadAll(form.Body)
	if !strings.Contains(string(body), "Choose your handle") {
		t.Fatalf("GET /handle body = %q, want the claim form", body)
	}

	claim := request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"ana-lucia"}}.Encode()), sessionCookie)
	claim.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if claim.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /handle status = %d, want 303", claim.StatusCode)
	}
	if got := claim.Header.Get("Location"); got != "/" {
		t.Errorf("POST /handle Location = %q, want %q", got, "/")
	}

	home := request(t, h, http.MethodGet, host, "/", nil, sessionCookie)
	defer home.Body.Close() //nolint:errcheck // httptest body close cannot fail
	homeBody, _ := io.ReadAll(home.Body)
	if !strings.Contains(string(homeBody), "https://ana-lucia.retrat.ar") {
		t.Errorf("home body = %q, want it to link the claimed page", homeBody)
	}
}

func TestClaimHandleSubmit_RejectsInvalidShape(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")

	resp := request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"Ana Lucia"}}.Encode()), sessionCookie)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "lowercase") {
		t.Errorf("body = %q, want it to explain the shape rule", body)
	}
}

func TestClaimHandleSubmit_RejectsTaken(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	firstCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), firstCookie).Body.Close()

	secondCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "bea@example.com")
	resp := request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), secondCookie)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

func TestClaimHandleSubmit_AlreadySetRedirectsHome(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()

	resp := request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"someone-else"}}.Encode()), sessionCookie)
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (already has a handle)", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/" {
		t.Errorf("Location = %q, want %q", got, "/")
	}
}

func TestClaimHandleForm_ActiveAccountRedirectsHome(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()

	resp := request(t, h, http.MethodGet, host, "/handle", nil, sessionCookie)
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (nothing left to claim)", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/" {
		t.Errorf("Location = %q, want %q", got, "/")
	}
}
