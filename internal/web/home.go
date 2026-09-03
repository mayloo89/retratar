package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/user"
)

type moodOption struct {
	Key      string
	Label    string
	Selected bool
}

// appHomeData is what app_home.html renders.
type appHomeData struct {
	Handle      string
	HasMood     bool
	MoodLabel   string
	MoodNote    string
	MoodOptions []moodOption
	Error       string
}

// handleAppHome is the signed-in landing page. An anonymous visitor is sent
// to sign in. An account with no handle yet sees the claim form instead of
// the dashboard — there is no page URL to show until one is claimed.
func (s *Server) handleAppHome(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if u.State != user.StateActive {
		s.renderTemplate(w, http.StatusOK, "claim_handle.html", claimHandleData{})
		return
	}

	s.renderAppHome(w, r, http.StatusOK, u, "")
}

// renderAppHome loads the account's current mood and renders the dashboard.
// errMsg, if non-empty, is shown alongside the mood form; [handleMoodSubmit]
// uses this with a non-200 status to redisplay the form after a rejected
// submission, the same shape handleLoginRequest uses for login_form.html.
func (s *Server) renderAppHome(w http.ResponseWriter, r *http.Request, status int, u user.User, errMsg string) {
	data := appHomeData{Handle: u.Handle, Error: errMsg}

	m, err := s.Moods.CurrentMood(r.Context(), u.ID)
	switch {
	case err == nil:
		data.HasMood = true
		data.MoodLabel = m.Key.Label()
		data.MoodNote = m.Note
	case errors.Is(err, mood.ErrNoMood):
		// No mood set yet — data already carries the empty state.
	default:
		s.Logger.ErrorContext(r.Context(), "get current mood", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	for _, k := range mood.AllKeys() {
		data.MoodOptions = append(data.MoodOptions, moodOption{
			Key: string(k), Label: k.Label(), Selected: data.HasMood && k == m.Key,
		})
	}

	s.renderTemplate(w, status, "app_home.html", data)
}
