package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/ogcard"
	"github.com/mayloo89/retratar/internal/user"
)

// pageData is what page.html renders.
//
// MoodClass is always "none" or one of mood.AllKeys() — never raw user
// input. HasMood/MoodClass/MoodLabel come from the account's stored mood,
// whose key is validated by mood.Key.Valid at write time and constrained by
// the database's CHECK on moods.mood_key besides. That is what makes it safe
// to drop straight into the page's CSS class, no escaping needed beyond
// html/template's own auto-escaping.
type pageData struct {
	Handle    string
	HasMood   bool
	MoodClass string
	MoodLabel string
	MoodNote  string

	// OGURL and OGImageURL are absolute — an og:image scrapers must resolve
	// without any base URL of their own to resolve it against. See
	// [config.Config.PageBaseURL].
	OGURL string
	// OGImageURL and OGDescription each feed the OG meta tags; page.html
	// also reuses OGDescription as the visible empty-state paragraph
	// ("Todavía no eligió...") rather than hardcoding that sentence a
	// second time — see [ogcard.EmptyStateText], its one source.
	OGImageURL    string
	OGDescription string
}

// handlePage renders the public page for the handle resolved from the
// request's hostname; see [HandleFrom].
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	handle, ok := HandleFrom(r.Context())
	if !ok {
		http.NotFound(w, r)
		return
	}

	u, err := s.Users.GetByHandle(r.Context(), handle)
	if err != nil {
		if errors.Is(err, user.ErrUserNotFound) {
			http.NotFound(w, r)
			return
		}
		s.Logger.ErrorContext(r.Context(), "get user by handle", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	base := s.Config.PageBaseURL(u.Handle)
	data := pageData{
		Handle:        u.Handle,
		MoodClass:     "none",
		OGURL:         base + "/",
		OGImageURL:    base + "/og.png",
		OGDescription: ogcard.EmptyStateText,
	}

	m, err := s.Moods.CurrentMood(r.Context(), u.ID)
	switch {
	case err == nil:
		data.HasMood = true
		data.MoodClass = string(m.Key)
		data.MoodLabel = m.Key.Label()
		data.MoodNote = m.Note
		data.OGDescription = m.Key.Label()
		if m.Note != "" {
			data.OGDescription += ": " + m.Note
		}
	case errors.Is(err, mood.ErrNoMood):
		// No mood set yet — data already carries the empty state.
	default:
		s.Logger.ErrorContext(r.Context(), "get current mood", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, http.StatusOK, "page.html", data)
}

// handleTheme serves the one stylesheet every public page loads.
func (s *Server) handleTheme(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(themeCSS)
}
