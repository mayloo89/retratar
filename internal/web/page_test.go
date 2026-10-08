package web_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/ogcard"
	"github.com/mayloo89/retratar/internal/web"
)

func TestHandlePage_RendersMoodForAClaimedHandle(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	appHost := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, appHost, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, appHost, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()
	request(t, h, http.MethodPost, appHost, "/mood",
		strings.NewReader(url.Values{"mood_key": {"inspirado"}, "note": {"nuevo proyecto"}}.Encode()),
		sessionCookie).Body.Close()

	page := request(t, h, http.MethodGet, "ana.retrat.ar", "/", nil)
	defer page.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if page.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", page.StatusCode)
	}
	body, err := io.ReadAll(page.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	for _, want := range []string{"mood-inspirado", "inspirado", "nuevo proyecto"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("page body = %q, want it to contain %q", body, want)
		}
	}
}

func TestHandlePage_NoMoodYetRendersEmptyState(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	appHost := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, appHost, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, appHost, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()

	page := request(t, h, http.MethodGet, "ana.retrat.ar", "/", nil)
	defer page.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if page.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", page.StatusCode)
	}
	body, _ := io.ReadAll(page.Body)
	if !strings.Contains(string(body), "mood-none") {
		t.Errorf("page body = %q, want the neutral mood-none class", body)
	}
}

func TestHandlePage_UnclaimedHandleIs404(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()

	resp := request(t, h, http.MethodGet, "nobody.retrat.ar", "/", nil)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleTheme_ServesCSS(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()

	resp := request(t, h, http.MethodGet, "ana.retrat.ar", "/theme.css", nil)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), ".mood-feliz") {
		t.Errorf("theme.css does not define .mood-feliz")
	}
}

// TestPage_EmitsAbsoluteOGTags checks the og: meta tags a link-preview
// scraper reads: absolute URLs (scrapers do not resolve relative ones) and a
// description that tracks the current mood. The note contains an apostrophe
// so the assertion covers html/template's attribute escaping (&#39;).
func TestPage_EmitsAbsoluteOGTags(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	appHost := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, appHost, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, appHost, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()

	getPage := func() string {
		t.Helper()
		resp := request(t, h, http.MethodGet, "ana.retrat.ar", "/", nil)
		defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return string(body)
	}
	meta := func(property, content string) string {
		return `<meta property="` + property + `" content="` + content + `">`
	}

	body := getPage()
	for _, want := range []string{
		meta("og:image", "https://ana.retrat.ar/og.png"),
		meta("og:url", "https://ana.retrat.ar/"),
		meta("og:description", ogcard.EmptyStateText),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("empty-state page missing %q\nbody = %s", want, body)
		}
	}

	request(t, h, http.MethodPost, appHost, "/mood",
		strings.NewReader(url.Values{"mood_key": {"inspirado"}, "note": {"it's a new project"}}.Encode()),
		sessionCookie).Body.Close()

	body = getPage()
	want := meta("og:description", mood.Key("inspirado").Label()+": it&#39;s a new project")
	if !strings.Contains(body, want) {
		t.Errorf("mood page missing %q\nbody = %s", want, body)
	}
}

func TestPage_LinksToRandom(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	claim(t, srv, sender, "ana@example.com", "ana")

	page := request(t, h, http.MethodGet, "ana.retrat.ar", "/", nil)
	defer page.Body.Close() //nolint:errcheck // httptest body close cannot fail
	body, _ := io.ReadAll(page.Body)
	if !strings.Contains(string(body), `href="https://retratar.com.ar/random"`) {
		t.Errorf("page body = %q, want a link to https://retratar.com.ar/random", body)
	}
}

// claimAna logs in and claims the handle "ana", returning the session cookie.
func claimAna(t *testing.T, srv *web.Server, sender *stubSender, h http.Handler) *http.Cookie {
	t.Helper()
	appHost := srv.Config.AppHost
	sessionCookie := completeLogin(t, h, appHost, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, appHost, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()
	return sessionCookie
}

func getBody(t *testing.T, h http.Handler, host, path string) string {
	t.Helper()
	resp := request(t, h, http.MethodGet, host, path, nil)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s%s status = %d, want 200", host, path, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func TestPage_RendersIdentityHeader(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	claimAna(t, srv, sender, h)

	body := getBody(t, h, "ana.retrat.ar", "/")
	for _, want := range []string{
		`<h1 class="blk-name">ana</h1>`,
		`<p class="blk-address">ana.retrat.ar</p>`,
		`<div class="blk-frame" aria-hidden="true">A</div>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page body = %q, want it to contain %q", body, want)
		}
	}
}

func TestPage_AddressFollowsConfig(t *testing.T) {
	srv, sender := newLoginServer(t)
	srv.Config.PagesHost = "pages.localhost"
	h := srv.Handler()
	claimAna(t, srv, sender, h)

	body := getBody(t, h, "ana.pages.localhost", "/")
	if want := `<p class="blk-address">ana.pages.localhost</p>`; !strings.Contains(body, want) {
		t.Errorf("page body = %q, want it to contain %q", body, want)
	}
	if strings.Contains(body, "retrat.ar") {
		t.Errorf("page body = %q, want no hardcoded retrat.ar", body)
	}
}

func TestThemeCSS_NoShadowNoFontFace(t *testing.T) {
	srv, _ := newLoginServer(t)

	css := getBody(t, srv.Handler(), "ana.retrat.ar", "/theme.css")
	for _, banned := range []string{"box-shadow", "@font-face"} {
		if strings.Contains(css, banned) {
			t.Errorf("theme.css contains %q, want none", banned)
		}
	}
}
