package web_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestLegalPages(t *testing.T) {
	srv, _ := newLoginServer(t)
	srv.Config.OperatorName = "Nombre De Prueba"
	srv.Config.ContactEmail = "contacto@example.com"
	h := srv.Handler()

	for _, tt := range []struct {
		path, h1 string
		extra    []string
	}{
		{"/privacy", "<h1>Política de privacidad</h1>", []string{"AGENCIA DE ACCESO A LA INFORMACIÓN PÚBLICA"}},
		{"/terms", "<h1>Términos</h1>", nil},
	} {
		t.Run(tt.path, func(t *testing.T) {
			resp := request(t, h, http.MethodGet, "retratar.com.ar", tt.path, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			body := readBody(t, resp)
			for _, want := range append([]string{
				`lang="es"`, tt.h1, "Nombre De Prueba", "contacto@example.com", "Última actualización",
			}, tt.extra...) {
				if !strings.Contains(body, want) {
					t.Errorf("%s lacks %q", tt.path, want)
				}
			}
			// Plain text, not a mailto: link.
			if strings.Contains(body, "mailto:") {
				t.Errorf("%s renders the email as a link", tt.path)
			}
		})
	}
}

// TestLegalPages_SameWhenSignedIn: a signed-in visitor sees the same page.
func TestLegalPages_SameWhenSignedIn(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	c := completeLogin(t, h, "retratar.com.ar", sender, srv.Config.BaseURL(), "ana@example.com")

	for _, path := range []string{"/privacy", "/terms"} {
		out := legalBody(t, h, path)
		in := legalBody(t, h, path, c)
		if in != out {
			t.Errorf("%s differs for a signed-in visitor", path)
		}
	}
}

func TestLegalLinks(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)
	pending := completeLogin(t, e.h, appHost, e.send, e.srv.Config.BaseURL(), "bob@example.com")

	app := map[string]string{
		"sign-in":   string(e.status(t, appHost, "/login").Body),
		"claim":     string(e.status(t, appHost, "/", pending).Body),
		"dashboard": string(e.status(t, appHost, "/", c).Body),
	}
	for name, body := range app {
		for _, want := range []string{`href="/privacy"`, `href="/terms"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s page lacks %s", name, want)
			}
		}
	}
	if !strings.Contains(app["dashboard"], `href="/account/delete"`) || !strings.Contains(app["dashboard"], "Tu cuenta") {
		t.Errorf("dashboard lacks the account box")
	}

	// The check-your-email page is the answer to a login request.
	r := e.form(t, nil, "/login", map[string][]string{"email": {"zoe@example.com"}})
	wantBodyHas(t, r, http.StatusOK, `href="/privacy"`)
	wantBodyHas(t, r, http.StatusOK, `href="/terms"`)

	// On a public page the links are absolute and point at the app host.
	page := e.status(t, "ana.retrat.ar", "/")
	for _, want := range []string{
		`<a href="https://retratar.com.ar/privacy">Privacidad</a>`,
		`<a href="https://retratar.com.ar/terms">Términos</a>`,
		`class="blk-legal"`,
	} {
		if !strings.Contains(string(page.Body), want) {
			t.Errorf("public page lacks %s", want)
		}
	}
}

// legalBody fetches path from the app host and returns the body.
func legalBody(t *testing.T, h http.Handler, path string, cookies ...*http.Cookie) string {
	t.Helper()
	resp := request(t, h, http.MethodGet, "retratar.com.ar", path, nil, cookies...)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	return readBody(t, resp)
}
