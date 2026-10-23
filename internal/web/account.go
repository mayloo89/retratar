package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mayloo89/retratar/internal/user"
)

// msgConfirmDelete is shown when the confirmation checkbox was not ticked.
const msgConfirmDelete = "Marcá la casilla para confirmar."

// accountDeleteData is what account_delete.html renders.
type accountDeleteData struct {
	// Handle is the account's handle, empty when it has none; the reservation
	// notice is shown only when there is one.
	Handle string
	Error  string
}

// handleAccountDeleteForm shows the confirmation page. Deleting is never one
// click: the dashboard links here, and only the POST below does anything.
func (s *Server) handleAccountDeleteForm(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.renderTemplate(w, http.StatusOK, "account_delete.html", accountDeleteData{Handle: u.Handle})
}

// handleAccountDelete deletes the signed-in account and everything it owns.
// An account that never picked a handle can delete too.
//
// The database work is one transaction ([user.Service.DeleteAccount]). The
// photo file goes only after it commits: if the transaction fails the account
// still exists, with its photo.
func (s *Server) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if r.FormValue("confirm") != "yes" {
		s.renderTemplate(w, http.StatusUnprocessableEntity, "account_delete.html",
			accountDeleteData{Handle: u.Handle, Error: msgConfirmDelete})
		return
	}

	photoKey, err := s.Users.DeleteAccount(r.Context(), u.ID, s.now())
	if err != nil {
		if errors.Is(err, user.ErrUserNotFound) {
			// Already gone, from a request that raced this one.
			ClearSessionCookie(w)
			http.Redirect(w, r, "/account/deleted", http.StatusSeeOther)
			return
		}
		s.Logger.ErrorContext(r.Context(), "delete account", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if photoKey != "" {
		s.removePhotoFile(r, photoKey)
	}
	ClearSessionCookie(w)
	http.Redirect(w, r, "/account/deleted", http.StatusSeeOther)
}

// handleAccountDeleted is the goodbye page. The session is gone by now, so it
// needs none.
func (s *Server) handleAccountDeleted(w http.ResponseWriter, _ *http.Request) {
	s.renderTemplate(w, http.StatusOK, "account_deleted.html", nil)
}
