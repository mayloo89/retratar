package web_test

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/config"
	"github.com/mayloo89/retratar/internal/mood"
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

// TestAppHome_NoHandleHasNoPageLink covers an account with no handle posting
// a mood. The dashboard that once re-rendered for it is now unreachable:
// POST /mood redirects to / and stores nothing. The {{with .PageURL}} guard in
// the template stays regardless, so a future path to the dashboard without a
// handle still cannot render an empty link.
func TestAppHome_NoHandleHasNoPageLink(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")
	id, err := srv.Sessions.Lookup(t.Context(), sessionCookie.Value)
	if err != nil {
		t.Fatalf("Sessions.Lookup() error = %v", err)
	}

	resp := request(t, h, http.MethodPost, host, "/mood",
		strings.NewReader(url.Values{"mood_key": {"euforico"}}.Encode()), sessionCookie)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if _, err := srv.Moods.CurrentMood(t.Context(), id); !errors.Is(err, mood.ErrNoMood) {
		t.Errorf("CurrentMood() error = %v, want ErrNoMood", err)
	}
}
