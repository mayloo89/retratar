package web_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestAppTemplatesAreSpanish renders every app page and checks it declares
// Spanish and carries none of the old English copy.
func TestAppTemplatesAreSpanish(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	// fetch performs a request and returns the response body, closing it
	// before returning.
	fetch := func(method, path string, body io.Reader, cookies ...*http.Cookie) string {
		t.Helper()
		resp := request(t, h, method, host, path, body, cookies...)
		defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return string(b)
	}

	pages := map[string]string{}

	pages["/login"] = fetch(http.MethodGet, "/login", nil)

	// The check-email page is the response to a login request; the same
	// request leaves the link in the stub sender for the confirm page.
	pages["check email"] = fetch(http.MethodPost, "/login",
		strings.NewReader(url.Values{"email": {"ana@example.com"}}.Encode()))
	token := sender.token(t, srv.Config.BaseURL())

	pages["confirm"] = fetch(http.MethodGet, "/login/"+token, nil)
	// GET /login/{token} always renders the confirm page; the invalid page
	// only appears when a POST arrives without the nonce cookie.
	pages["invalid"] = fetch(http.MethodPost, "/login/not-a-real-token",
		strings.NewReader(url.Values{"nonce": {"x"}}.Encode()))

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
	pages["/handle"] = fetch(http.MethodGet, "/handle", nil, sessionCookie)

	claim := request(t, h, http.MethodPost, host, "/handle",
		strings.NewReader(url.Values{"handle": {"ana-lucia"}}.Encode()), sessionCookie)
	claim.Body.Close() //nolint:errcheck // httptest body close cannot fail
	pages["/"] = fetch(http.MethodGet, "/", nil, sessionCookie)

	if !strings.Contains(pages["confirm"], "Confirmá que sos vos") {
		t.Errorf("confirm: body is not the confirm page: %q", pages["confirm"])
	}
	if !strings.Contains(pages["invalid"], "Este link ya no sirve") {
		t.Errorf("invalid: body is not the invalid-link page: %q", pages["invalid"])
	}

	english := []string{"Sign in", "Your page", "Update mood", "Check your inbox"}
	for name, body := range pages {
		if !strings.Contains(body, `lang="es"`) {
			t.Errorf("%s: body has no lang=\"es\": %q", name, body)
		}
		for _, old := range english {
			if strings.Contains(body, old) {
				t.Errorf("%s: body still contains %q", name, old)
			}
		}
	}
}
