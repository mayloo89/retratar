package web_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func postName(t *testing.T, h http.Handler, host, name string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	return request(t, h, http.MethodPost, host, "/name",
		strings.NewReader(url.Values{"name": {name}}.Encode()), cookies...)
}

func TestDashboard_SetDisplayName(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"
	sessionCookie := claimAna(t, srv, sender, h)

	resp := postName(t, h, host, "Seba", sessionCookie)
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /name status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}

	home := request(t, h, http.MethodGet, host, "/", nil, sessionCookie)
	defer home.Body.Close() //nolint:errcheck // httptest body close cannot fail
	body, _ := io.ReadAll(home.Body)
	if !strings.Contains(string(body), `value="Seba"`) {
		t.Errorf("dashboard = %q, want the name field to hold Seba", body)
	}

	page := getBody(t, h, "ana.retrat.ar", "/")
	for _, want := range []string{
		`<h1 class="blk-name">Seba</h1>`,
		`<title>Seba</title>`,
		`<meta property="og:title" content="Seba">`,
		`<p class="blk-address">ana.retrat.ar</p>`,
		`Firmá el libro de Seba`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page = %q, want it to contain %q", page, want)
		}
	}
}

func TestDashboard_DisplayNameIsEscaped(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	sessionCookie := claimAna(t, srv, sender, h)

	postName(t, h, "retratar.com.ar", `<b>"x"</b>`, sessionCookie).Body.Close()

	page := getBody(t, h, "ana.retrat.ar", "/")
	if strings.Contains(page, "<b>") {
		t.Errorf("page = %q, want the name escaped, found a raw <b>", page)
	}
	if !strings.Contains(page, `<h1 class="blk-name">&lt;b&gt;`) {
		t.Errorf("page = %q, want the escaped name in blk-name", page)
	}
	if !strings.Contains(page, `content="&lt;b&gt;`) {
		t.Errorf("page = %q, want the escaped name in og:title", page)
	}
}

func TestDashboard_ClearDisplayName(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"
	sessionCookie := claimAna(t, srv, sender, h)

	postName(t, h, host, "Seba", sessionCookie).Body.Close()
	resp := postName(t, h, host, "", sessionCookie)
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /name status = %d, want 303", resp.StatusCode)
	}

	page := getBody(t, h, "ana.retrat.ar", "/")
	for _, want := range []string{`<h1 class="blk-name">ana</h1>`, `<title>ana</title>`} {
		if !strings.Contains(page, want) {
			t.Errorf("page = %q, want it to contain %q", page, want)
		}
	}
}

func TestDashboard_InvalidDisplayName(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"
	sessionCookie, id := signedInWithHandle(t, srv, h, sender, host)
	long := strings.Repeat("a", 41)

	resp := postName(t, h, host, long, sessionCookie)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Escribí hasta 40 caracteres, sin saltos de línea.") {
		t.Errorf("body = %q, want the error text", body)
	}
	if !strings.Contains(string(body), `value="`+long+`"`) {
		t.Errorf("body = %q, want the typed value kept", body)
	}

	u, err := srv.Users.GetByID(t.Context(), id)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if u.DisplayName != "" {
		t.Errorf("DisplayName = %q, want nothing stored", u.DisplayName)
	}
}

func TestNameSubmit_Guards(t *testing.T) {
	host := "retratar.com.ar"

	t.Run("signed out", func(t *testing.T) {
		srv, _ := newLoginServer(t)
		resp := postName(t, srv.Handler(), host, "Seba")
		resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
			t.Errorf("status = %d, Location = %q, want 303 /login", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	t.Run("no handle", func(t *testing.T) {
		srv, sender := newLoginServer(t)
		h := srv.Handler()
		sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
		id, err := srv.Sessions.Lookup(t.Context(), sessionCookie.Value)
		if err != nil {
			t.Fatalf("Sessions.Lookup() error = %v", err)
		}
		resp := postName(t, h, host, "Seba", sessionCookie)
		resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
			t.Errorf("status = %d, Location = %q, want 303 /", resp.StatusCode, resp.Header.Get("Location"))
		}
		if u, _ := srv.Users.GetByID(t.Context(), id); u.DisplayName != "" {
			t.Errorf("DisplayName = %q, want nothing stored without a handle", u.DisplayName)
		}
	})

	t.Run("cross origin", func(t *testing.T) {
		srv, sender := newLoginServer(t)
		h := srv.Handler()
		sessionCookie, id := signedInWithHandle(t, srv, h, sender, host)
		resp := requestWithBodyHeaders(t, h, map[string]string{"Sec-Fetch-Site": "cross-site"},
			http.MethodPost, host, "/name", strings.NewReader(url.Values{"name": {"Seba"}}.Encode()), sessionCookie)
		resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
		if u, _ := srv.Users.GetByID(t.Context(), id); u.DisplayName != "" {
			t.Errorf("DisplayName = %q, want nothing stored after a refused POST", u.DisplayName)
		}
	})
}

func TestPage_InitialFollowsName(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	sessionCookie := claimAna(t, srv, sender, h)

	postName(t, h, "retratar.com.ar", "élena", sessionCookie).Body.Close()

	page := getBody(t, h, "ana.retrat.ar", "/")
	if want := `<div class="blk-frame" aria-hidden="true">É</div>`; !strings.Contains(page, want) {
		t.Errorf("page = %q, want it to contain %q", page, want)
	}
}

func TestOGImage_ETagChangesWithName(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	sessionCookie := claimAna(t, srv, sender, h)

	etagOf := func() string {
		resp := request(t, h, http.MethodGet, "ana.retrat.ar", "/og.png", nil)
		resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
		return resp.Header.Get("ETag")
	}

	before := etagOf()
	postName(t, h, "retratar.com.ar", "Seba", sessionCookie).Body.Close()
	after := etagOf()
	if before == "" || after == "" || before == after {
		t.Errorf("ETag before = %q, after = %q, want two different non-empty values", before, after)
	}
}
