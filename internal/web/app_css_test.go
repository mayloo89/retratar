package web_test

import (
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/ogcard"
)

// bodyOf performs a request and returns the response body, closing it.
func bodyOf(t *testing.T, h http.Handler, method, path string, body io.Reader, cookies ...*http.Cookie) string {
	t.Helper()
	resp := request(t, h, method, guestbookAppHost, path, body, cookies...)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	return readBody(t, resp)
}

func TestAppCSS_Served(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()

	resp := request(t, h, http.MethodGet, guestbookAppHost, "/app.css", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /app.css status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/css; charset=utf-8", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q, want public, max-age=3600", got)
	}
	if body := readBody(t, resp); body == "" {
		t.Error("GET /app.css body is empty")
	}

	pages := request(t, h, http.MethodGet, "ana.retrat.ar", "/app.css", nil)
	defer pages.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if pages.StatusCode == http.StatusOK {
		t.Errorf("GET /app.css on a pages host status = %d, want it not served", pages.StatusCode)
	}
}

// TestAppCSS_MoodRulesMatchTheme keeps the app stylesheet's mood colours from
// drifting away from the ones every public page loads.
func TestAppCSS_MoodRulesMatchTheme(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()

	app, err := ogcard.ParsePalette([]byte(getBody(t, h, guestbookAppHost, "/app.css")))
	if err != nil {
		t.Fatalf("ParsePalette(app.css) error = %v", err)
	}
	theme, err := ogcard.ParsePalette([]byte(getBody(t, h, "ana.retrat.ar", "/theme.css")))
	if err != nil {
		t.Fatalf("ParsePalette(theme.css) error = %v", err)
	}
	if !reflect.DeepEqual(app, theme) {
		t.Errorf("app.css mood rules = %v, want them equal to theme.css's %v", app, theme)
	}
}

func TestAppCSS_NoShadowNoFontFace(t *testing.T) {
	srv, _ := newLoginServer(t)
	css := getBody(t, srv.Handler(), guestbookAppHost, "/app.css")
	for _, banned := range []string{"box-shadow", "@font-face"} {
		if strings.Contains(css, banned) {
			t.Errorf("app.css contains %q, want none", banned)
		}
	}
}

func TestAppTemplates_LinkStylesheet(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()

	pages := map[string]string{}
	pages["/login"] = getBody(t, h, guestbookAppHost, "/login")

	pages["check email"] = bodyOf(t, h, http.MethodPost, "/login",
		strings.NewReader(url.Values{"email": {"ana@example.com"}}.Encode()))
	token := sender.token(t, srv.Config.BaseURL())
	pages["confirm"] = getBody(t, h, guestbookAppHost, "/login/"+token)
	pages["invalid"] = bodyOf(t, h, http.MethodPost, "/login/not-a-real-token",
		strings.NewReader(url.Values{"nonce": {"x"}}.Encode()))

	noHandle, _ := guestbookAccount(t, srv, "b@example.com", "")
	owner, _ := guestbookAccount(t, srv, "c@example.com", "carla")
	pages["/handle"] = bodyOf(t, h, http.MethodGet, "/handle", nil, noHandle)
	pages["/"] = bodyOf(t, h, http.MethodGet, "/", nil, owner)
	pages["/sign/carla"] = getBody(t, h, guestbookAppHost, "/sign/carla")

	for name, body := range pages {
		for _, want := range []string{`href="/app.css"`, `<meta name="viewport" content="width=device-width, initial-scale=1">`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: body does not contain %q: %q", name, want, body)
			}
		}
	}
}

func TestDashboard_MoodClass(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	cookie, _ := guestbookAccount(t, srv, "a@example.com", "a")

	before := bodyOf(t, h, http.MethodGet, "/", nil, cookie)
	// The radio labels carry mood- classes, so look at the <body> tag itself.
	if !strings.Contains(before, "<body>") || strings.Contains(before, "<body ") {
		t.Errorf("dashboard with no mood = %q, want a bare <body> with no mood- class", before)
	}

	post := request(t, h, http.MethodPost, guestbookAppHost, "/mood",
		strings.NewReader(url.Values{"mood_key": {"inspirado"}}.Encode()), cookie)
	post.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if post.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /mood status = %d, want 303", post.StatusCode)
	}

	after := bodyOf(t, h, http.MethodGet, "/", nil, cookie)
	if !strings.Contains(after, `<body class="mood-inspirado">`) {
		t.Errorf("dashboard = %q, want <body class=\"mood-inspirado\">", after)
	}
	if !strings.Contains(after, `value="inspirado" checked`) {
		t.Errorf("dashboard = %q, want the inspirado radio checked", after)
	}
	if n := strings.Count(after, " checked"); n != 1 {
		t.Errorf("dashboard has %d checked radios, want exactly 1", n)
	}
	if n := strings.Count(after, `type="radio"`); n != len(mood.AllKeys()) {
		t.Errorf("dashboard has %d radios, want %d", n, len(mood.AllKeys()))
	}
}

func TestSignPage_UsesOwnerMood(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	_, owner := guestbookAccount(t, srv, "a@example.com", "a")
	signerCookie, signer := guestbookAccount(t, srv, "b@example.com", "b")
	if _, err := srv.Moods.SetMood(t.Context(), owner.ID, mood.Key("enamorado"), ""); err != nil {
		t.Fatalf("SetMood(owner) error = %v", err)
	}
	if _, err := srv.Moods.SetMood(t.Context(), signer.ID, mood.Key("feliz"), ""); err != nil {
		t.Fatalf("SetMood(signer) error = %v", err)
	}

	get := bodyOf(t, h, http.MethodGet, "/sign/a", nil, signerCookie)
	if !strings.Contains(get, `<body class="mood-enamorado">`) {
		t.Errorf("sign page = %q, want the owner's mood-enamorado", get)
	}
	if strings.Contains(get, "mood-feliz") {
		t.Errorf("sign page = %q, must not wear the signer's mood-feliz", get)
	}

	resp := request(t, h, http.MethodPost, guestbookAppHost, "/sign/a", signForm(strings.Repeat("ñ", 281)), signerCookie)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("POST /sign/a status = %d, want 422", resp.StatusCode)
	}
	if rerender := readBody(t, resp); !strings.Contains(rerender, `<body class="mood-enamorado">`) {
		t.Errorf("re-rendered sign page = %q, want the owner's mood-enamorado", rerender)
	}
}

func TestDashboard_HiddenEntryTag(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	aCookie, a := guestbookAccount(t, srv, "a@example.com", "a")
	_, b := guestbookAccount(t, srv, "b@example.com", "b")
	entry, err := srv.Guestbook.Sign(t.Context(), a.ID, b.ID, "entrada oculta")
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	shown := bodyOf(t, h, http.MethodGet, "/", nil, aCookie)
	if strings.Contains(shown, `class="hidden"`) || strings.Contains(shown, `<span class="tag">oculto</span>`) {
		t.Errorf("dashboard = %q, want no hidden marks on a visible entry", shown)
	}

	if err = srv.Guestbook.Hide(t.Context(), a.ID, entry.ID); err != nil {
		t.Fatalf("Hide() error = %v", err)
	}
	hidden := bodyOf(t, h, http.MethodGet, "/", nil, aCookie)
	for _, want := range []string{`<li class="hidden">`, `<span class="tag">oculto</span>`} {
		if !strings.Contains(hidden, want) {
			t.Errorf("dashboard = %q, want it to contain %q", hidden, want)
		}
	}
}

// TestDashboard_LongHandleEntry only proves the markup path for a handle at
// the 30-character limit; the layout fix (a shrinkable grid track) is CSS and
// cannot be asserted from Go.
func TestDashboard_LongHandleEntry(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	aCookie, a := guestbookAccount(t, srv, "a@example.com", "a")
	long := strings.Repeat("x", 30)
	_, b := guestbookAccount(t, srv, "b@example.com", long)
	if _, err := srv.Guestbook.Sign(t.Context(), a.ID, b.ID, "hola"); err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	body := bodyOf(t, h, http.MethodGet, "/", nil, aCookie)
	if !strings.Contains(body, ">"+long+"</a>") {
		t.Errorf("dashboard = %q, want the 30-character handle rendered in full", body)
	}
}
