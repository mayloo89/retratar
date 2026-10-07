package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/mayloo89/retratar/internal/user"
)

// handleRandom sends the visitor to a random claimed page. A signed-in visitor
// is never sent to their own. With no eligible page it goes back to the app
// home.
//
// The redirect is a 302 so browsers do not cache it, and PrivateCache already
// marks every app-surface response no-store.
func (s *Server) handleRandom(w http.ResponseWriter, r *http.Request) {
	var exclude *uuid.UUID
	if u, ok := UserFrom(r.Context()); ok {
		exclude = &u.ID
	}

	handle, err := s.Users.RandomHandle(r.Context(), exclude)
	switch {
	case err == nil:
		http.Redirect(w, r, s.Config.PageBaseURL(handle)+"/", http.StatusFound)
	case errors.Is(err, user.ErrUserNotFound):
		http.Redirect(w, r, "/", http.StatusFound)
	default:
		s.Logger.ErrorContext(r.Context(), "random handle", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
