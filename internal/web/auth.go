package web

import (
	"errors"
	"net/http"

	"github.com/mayloo89/retratar/internal/session"
	"github.com/mayloo89/retratar/internal/user"
)

// CurrentUser resolves the session cookie, if any, into the account making
// the request and attaches it to the context; see [UserFrom].
//
// A missing or invalid cookie is not an error here — the request just
// continues unauthenticated. Whether a route requires a session is that
// route's own decision, not this middleware's. A lookup failure that isn't
// [session.ErrInvalidSession] or [user.ErrUserNotFound] — a database outage,
// say — is a real error and fails the request instead of quietly looking
// like everyone logged out. This is wired into the app surface only; see
// [Server.Handler]. The pages surface never resolves a user because it never
// reads the session cookie at all.
func CurrentUser(sessions *session.Service, users *user.Service) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)
				return
			}

			userID, err := sessions.Lookup(r.Context(), cookie.Value)
			if err != nil {
				if !errors.Is(err, session.ErrInvalidSession) {
					http.Error(w, "internal server error", http.StatusInternalServerError)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			u, err := users.GetByID(r.Context(), userID)
			if err != nil {
				if !errors.Is(err, user.ErrUserNotFound) {
					http.Error(w, "internal server error", http.StatusInternalServerError)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
		})
	}
}
