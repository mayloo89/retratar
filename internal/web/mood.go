package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/user"
)

// noteBreaks turns the line breaks a textarea sends into single spaces. The
// note is one line ([mood.NormaliseNote] stays strict for every other caller),
// but the dashboard's field is a textarea, where Enter is natural. Only the
// breaks change: the user's own spaces stay as typed.
var noteBreaks = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")

// handleMoodSubmit sets the signed-in account's current mood.
func (s *Server) handleMoodSubmit(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// The dashboard and its mood form exist only after a handle is claimed
	// (see handleAppHome). A mood without a page has nowhere to be shown.
	if u.State != user.StateActive {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest)
		return
	}

	key := mood.Key(r.FormValue("mood_key"))
	note := noteBreaks.Replace(r.FormValue("note"))

	if _, err := s.Moods.SetMood(r.Context(), u.ID, key, note); err != nil {
		switch {
		case errors.Is(err, mood.ErrInvalidMood):
			s.renderAppHome(w, r, http.StatusUnprocessableEntity, u, "Elegí uno de los estados de la lista.")
		case errors.Is(err, mood.ErrInvalidNote):
			s.renderAppHome(w, r, http.StatusUnprocessableEntity, u, "La nota puede tener hasta 60 caracteres, en una sola línea.")
		default:
			s.Logger.ErrorContext(r.Context(), "set mood", slog.String("error", err.Error()))
			s.renderError(w, r, http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, "/?ok=mood#estado", http.StatusSeeOther)
}
