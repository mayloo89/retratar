package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mayloo89/retratar/internal/mood"
)

// handleMoodSubmit sets the signed-in account's current mood.
func (s *Server) handleMoodSubmit(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	key := mood.Key(r.FormValue("mood_key"))
	note := r.FormValue("note")

	if _, err := s.Moods.SetMood(r.Context(), u.ID, key, note); err != nil {
		switch {
		case errors.Is(err, mood.ErrInvalidMood):
			s.renderAppHome(w, r, http.StatusUnprocessableEntity, u, "Choose one of the moods listed.")
		case errors.Is(err, mood.ErrInvalidNote):
			s.renderAppHome(w, r, http.StatusUnprocessableEntity, u, "Note must be 60 characters or fewer, and one line.")
		default:
			s.Logger.ErrorContext(r.Context(), "set mood", slog.String("error", err.Error()))
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
