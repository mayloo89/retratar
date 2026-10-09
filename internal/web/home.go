package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mayloo89/retratar/internal/guestbook"
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
	DisplayName string
	NameError   string
	PageURL     string
	HasMood     bool
	MoodLabel   string
	MoodNote    string
	MoodOptions []moodOption
	Error       string

	// Guestbook is the owner's entries of any state, newest first.
	Guestbook []ownEntryView
}

// ownEntryView is one guestbook entry as the owner's dashboard renders it.
// Body is plain text; html/template escapes it.
type ownEntryView struct {
	ID           string
	AuthorHandle string
	AuthorURL    string
	Date         string
	Body         string
	Hidden       bool
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
	s.renderAppHomeErrors(w, r, status, u, errMsg, "")
}

// renderAppHomeErrors is [Server.renderAppHome] with a second message, shown
// next to the name form instead of the mood form. [handleNameSubmit] passes
// u with the rejected text in DisplayName so the field keeps what was typed.
func (s *Server) renderAppHomeErrors(w http.ResponseWriter, r *http.Request, status int, u user.User, errMsg, nameErr string) {
	data := appHomeData{Handle: u.Handle, DisplayName: u.DisplayName, NameError: nameErr, Error: errMsg}
	// An account without a handle has no page yet; POST /mood can still
	// re-render this dashboard for one, so leave PageURL empty for it.
	if u.Handle != "" {
		data.PageURL = s.Config.PageBaseURL(u.Handle)
	}

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

	// A guestbook that fails to load must not take the dashboard down with
	// it: log it and render the dashboard without the section. An account
	// without a handle has no page, hence no guestbook.
	if u.Handle != "" {
		entries, err := s.Guestbook.Own(r.Context(), u.ID, guestbook.OwnerLimit)
		if err != nil {
			s.Logger.ErrorContext(r.Context(), "list own guestbook entries", slog.String("error", err.Error()))
		}
		for _, e := range entries {
			data.Guestbook = append(data.Guestbook, ownEntryView{
				ID:           e.ID.String(),
				AuthorHandle: e.AuthorHandle,
				AuthorURL:    s.Config.PageBaseURL(e.AuthorHandle) + "/",
				Date:         e.CreatedAt.In(argentina).Format(guestbookDateLayout),
				Body:         e.Body,
				Hidden:       e.Hidden,
			})
		}
	}

	s.renderTemplate(w, status, "app_home.html", data)
}
