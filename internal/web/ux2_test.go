package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestLanding_AnonymousRoot(t *testing.T) {
	e := newPhotoEnv(t)

	r := e.status(t, appHost, "/")
	wantStatus(t, r, http.StatusOK)
	wantContains(t, string(r.Body),
		`<form method="post" action="/login"`,
		`name="email"`,
		`autocomplete="email"`,
		`href="/random"`,
		`<title>Tu página, a tu manera · retratar</title>`,
		`<meta name="description" content="Una página personal y simple`,
	)
}

func TestWordmark_ShowsTheFavicon(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	mark := `<a class="mark" href="/"><img src="/favicon.svg" width="20" height="20" alt="">retratar</a>`
	for _, path := range []string{"/", "/account/delete", "/privacy", "/sign/ana"} {
		wantContains(t, string(e.status(t, appHost, path, c).Body), mark)
	}
	// Signed out: the landing page and the login form.
	for _, path := range []string{"/", "/login"} {
		wantContains(t, string(e.status(t, appHost, path).Body), mark)
	}
	wantContains(t, string(e.status(t, appHost, "/nada-de-nada").Body), `<img src="/favicon.svg" width="20" height="20" alt="">retratar`)
}

func TestDashboard_StyleFoldOpensWhenThereIsSomethingToShow(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	if home := e.home(t, c); !strings.Contains(home, `<details class="fold">`) {
		t.Errorf("plain GET /: want a closed fold, got %s", regexp.MustCompile(`<details[^>]*>`).FindString(home))
	}

	r := e.status(t, appHost, "/?ok=style", c)
	wantContains(t, string(r.Body), `<details class="fold" open>`, "Listo. Tu estilo ya está publicado.")

	f := styleForm("none", nil)
	f.Set("action", "edit")
	r = e.form(t, c, "/style", f)
	wantStatus(t, r, http.StatusOK)
	wantContains(t, string(r.Body), `<details class="fold" open>`)

	// The notice stays outside the fold, before it.
	body := string(e.status(t, appHost, "/?ok=style", c).Body)
	if notice, fold := strings.Index(body, "Listo. Tu estilo"), strings.Index(body, "<details"); notice < 0 || notice > fold {
		t.Errorf("notice at %d, fold at %d, want the notice before the fold", notice, fold)
	}
}

func TestDashboard_StyleFoldSummaryStatus(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	wantContains(t, e.home(t, c), `Colores y tipografías <span class="st muted">Por defecto</span>`)
	e.publish(t, c, "none", with("c_bg", "#ff00cc"))
	wantContains(t, e.home(t, c), `Colores y tipografías <span class="st own">Personalizado</span>`)
}

func TestDashboard_JumpBarMatchesTheBoxes(t *testing.T) {
	e := newPhotoEnv(t)
	withPhoto := e.signUp(t, "ana@example.com", "ana", true)
	without := e.signUp(t, "bob@example.com", "bob", false)

	nav := regexp.MustCompile(`(?s)<nav class="jump" aria-label="Ir a">(.*?)</nav>`)
	href := regexp.MustCompile(`href="#([^"]+)"`)
	for name, tt := range map[string]struct {
		cookie    *http.Cookie
		wantFoto  bool
		wantCount int
	}{"with photo box": {withPhoto, true, 5}, "without photo box": {without, false, 4}} {
		body := e.home(t, tt.cookie)
		m := nav.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s: no jump bar", name)
		}
		links := href.FindAllStringSubmatch(m[1], -1)
		if len(links) != tt.wantCount {
			t.Errorf("%s: %d jump links, want %d", name, len(links), tt.wantCount)
		}
		for _, l := range links {
			if !strings.Contains(body, `id="`+l[1]+`"`) {
				t.Errorf("%s: jump link #%s has no matching id", name, l[1])
			}
		}
		if got := strings.Contains(m[1], `href="#foto"`); got != tt.wantFoto {
			t.Errorf("%s: Foto link = %v, want %v", name, got, tt.wantFoto)
		}
	}
}

func TestDashboard_NoteIsATwoLineTextarea(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	wantContains(t, e.home(t, c),
		`<textarea class="note" id="note" name="note" maxlength="60" rows="2">`,
		`<label for="note">Una nota (opcional)</label>`,
		`<p class="help">Hasta 60 caracteres.</p>`)
}

func TestMoodSubmit_NoteLineBreaksBecomeSpaces(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	r := e.form(t, c, "/mood", url.Values{"mood_key": {"feliz"}, "note": {"hola\r\nchau  vos\nyo\rtú"}})
	wantStatus(t, r, http.StatusSeeOther)
	if loc := r.Header.Get("Location"); loc != "/?ok=mood#estado" {
		t.Errorf("Location = %q, want /?ok=mood#estado", loc)
	}
	// Inner spaces the user typed stay as typed.
	wantContains(t, e.home(t, c), ">hola chau  vos yo tú</textarea>")
}

func TestEmptyStateCopy(t *testing.T) {
	e := newPhotoEnv(t)
	e.signUp(t, "ana@example.com", "ana", false)

	body := string(e.status(t, "ana.retrat.ar", "/").Body)
	wantContains(t, body, "Todavía no contó cómo está.", "Nadie firmó todavía. Podés ser la primera persona.")
}

func TestSignPage_SaysWhoseBookItIs(t *testing.T) {
	e := newPhotoEnv(t)
	ana := e.signUp(t, "ana@example.com", "ana", false)
	bob := e.signUp(t, "bob@example.com", "bob", false)
	wantStatus(t, e.form(t, ana, "/name", url.Values{"name": {"álvaro Luna"}}), http.StatusSeeOther)

	body := string(e.status(t, appHost, "/sign/ana", bob).Body)
	wantContains(t, body,
		`<title>Firmá el libro de álvaro Luna · retratar</title>`,
		`<span class="frame" aria-hidden="true">Á</span>`,
		`<strong>álvaro Luna</strong><span>ana.retrat.ar</span>`,
		`<h1>Firmá su libro</h1>`,
		`Hasta 280 caracteres. Va a aparecer en la página de álvaro Luna, con tu dirección.`)
}
