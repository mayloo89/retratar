package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mayloo89/retratar/internal/user"
)

// claimHandleData is what claim_handle.html renders.
type claimHandleData struct {
	Error string
	// Handle is what was typed, kept when the form is shown again.
	Handle string
}

// msgAdultRequired is shown when the 18+ and terms checkbox was not ticked.
const msgAdultRequired = "Para usar retratar tenés que tener 18 años o más y aceptar los términos."

// handleClaimForm shows the one-time handle-picker. An account that already
// has a handle has nothing left to claim, so it is sent to the dashboard.
func (s *Server) handleClaimForm(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if u.State == user.StateActive {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderTemplate(w, http.StatusOK, "claim_handle.html", claimHandleData{})
}

// handleClaimSubmit spends the one-time handle claim. Every way of not
// getting a handle out of this re-renders the same form with a reason, same
// shape as handleLoginRequest.
func (s *Server) handleClaimSubmit(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	handle := r.FormValue("handle")
	// The checkbox is required in the form; this is the server's own check.
	// Only exactly "yes" counts.
	if r.FormValue("adult") != "yes" {
		s.renderTemplate(w, http.StatusUnprocessableEntity, "claim_handle.html",
			claimHandleData{Error: msgAdultRequired, Handle: handle})
		return
	}

	_, err := s.Users.ClaimHandle(r.Context(), u.ID, handle, s.now())
	if err != nil {
		switch {
		case errors.Is(err, user.ErrInvalidHandle):
			s.renderTemplate(w, http.StatusUnprocessableEntity, "claim_handle.html",
				claimHandleData{Error: "Usá solo minúsculas, números y guiones."})
		case errors.Is(err, user.ErrHandleTaken):
			s.renderTemplate(w, http.StatusConflict, "claim_handle.html",
				claimHandleData{Error: "Esa dirección ya está tomada. Probá otra."})
		case errors.Is(err, user.ErrHandleAlreadySet):
			// Already claimed by a previous request — nothing left to do but
			// send them where a claimed account belongs.
			http.Redirect(w, r, "/", http.StatusSeeOther)
		default:
			s.Logger.ErrorContext(r.Context(), "claim handle", slog.String("error", err.Error()))
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
