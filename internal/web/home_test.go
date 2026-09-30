package web_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/config"
)

func TestAppHome_LinksToPageBaseURL(t *testing.T) {
	for _, env := range []config.Environment{config.EnvProduction, config.EnvDevelopment} {
		t.Run(string(env), func(t *testing.T) {
			srv, sender := newLoginServer(t)
			srv.Config.Env = env
			h := srv.Handler()
			host := "retratar.com.ar"

			sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
			request(t, h, http.MethodPost, host, "/handle",
				strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()

			resp := request(t, h, http.MethodGet, host, "/", nil, sessionCookie)
			defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
			body, _ := io.ReadAll(resp.Body)

			want := `href="` + srv.Config.PageBaseURL("ana") + `"`
			if !strings.Contains(string(body), want) {
				t.Errorf("home body = %s, want it to contain %q", body, want)
			}
		})
	}

	// Production must additionally give the literal https URL.
	srv, _ := newLoginServer(t)
	if got := srv.Config.PageBaseURL("ana"); got != "https://ana.retrat.ar" {
		t.Errorf("production PageBaseURL = %q, want https://ana.retrat.ar", got)
	}
}

// TestAppHome_NoHandleHasNoPageLink covers the one way the dashboard can
// render for an account with no handle: POST /mood with an invalid mood
// answers 422 by re-rendering it. There is no page to link to.
func TestAppHome_NoHandleHasNoPageLink(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")

	resp := request(t, h, http.MethodPost, host, "/mood",
		strings.NewReader(url.Values{"mood_key": {"euforico"}}.Encode()), sessionCookie)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, bad := range []string{`href=""`, ".retrat.ar"} {
		if strings.Contains(string(body), bad) {
			t.Errorf("body contains %q, want no page link for an account without a handle\nbody = %s", bad, body)
		}
	}
}
