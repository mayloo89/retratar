package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mayloo89/retratar/internal/user"
)

// handleNameSubmit sets or clears the signed-in account's display name.
func (s *Server) handleNameSubmit(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// The dashboard and its name form exist only after a handle is claimed
	// (see handleAppHome), and a name without a page has nowhere to be shown.
	if u.State != user.StateActive {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	raw := r.FormValue("name")

	if _, err := s.Users.SetDisplayName(r.Context(), u.ID, raw); err != nil {
		if errors.Is(err, user.ErrInvalidDisplayName) {
			// Show the rejected text back in the field, not the stored name.
			u.DisplayName = raw
			s.renderAppHomeErrors(w, r, http.StatusUnprocessableEntity, u, "", "Escribí hasta 40 caracteres, sin saltos de línea.")
			return
		}
		s.Logger.ErrorContext(r.Context(), "set display name", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
