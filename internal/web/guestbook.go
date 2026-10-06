package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mayloo89/retratar/internal/guestbook"
	"github.com/mayloo89/retratar/internal/user"
)

// guestbookSignData is what guestbook_sign.html renders. Exactly one of the
// states applies: the form (CanSign), or a short explanation of why not.
type guestbookSignData struct {
	Handle  string
	PageURL string

	// Message and its link explain why the visitor has no form. CanSign is
	// true only when the visitor may write, and then Body and Error carry a
	// rejected attempt back into the form.
	CanSign     bool
	Message     string
	MessageURL  string
	MessageLink string
	Body        string
	Error       string
}

// guestbookOwner resolves the {handle} path value to the page owner. The
// stored handle, not the raw path value, is what every URL is built from.
// It writes the response itself and reports false when there is no owner.
func (s *Server) guestbookOwner(w http.ResponseWriter, r *http.Request) (user.User, bool) {
	owner, err := s.Users.GetByHandle(r.Context(), r.PathValue("handle"))
	if err != nil {
		if errors.Is(err, user.ErrUserNotFound) {
			http.NotFound(w, r)
			return user.User{}, false
		}
		s.Logger.ErrorContext(r.Context(), "get user by handle", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return user.User{}, false
	}
	return owner, true
}

// guestbookState works out what the visitor may do on owner's sign page.
// ok is true when they may write; otherwise data already carries the
// explanation and status is 200 on GET.
func (s *Server) guestbookState(r *http.Request, owner user.User) (data guestbookSignData, signer user.User, ok bool) {
	data = guestbookSignData{
		Handle:  owner.Handle,
		PageURL: s.Config.PageBaseURL(owner.Handle) + "/",
	}
	signer, signedIn := UserFrom(r.Context())
	switch {
	case !signedIn:
		data.Message = "Ingresá para firmar el libro de " + owner.Handle + "."
		data.MessageURL, data.MessageLink = "/login", "Ingresar"
	case signer.State != user.StateActive:
		data.Message = "Elegí tu nombre de usuario para firmar."
		data.MessageURL, data.MessageLink = "/", "Elegir nombre de usuario"
	case signer.ID == owner.ID:
		data.Message = "No podés firmar tu propio libro."
		data.MessageURL, data.MessageLink = data.PageURL, "Volver a tu página"
	default:
		data.CanSign = true
		return data, signer, true
	}
	return data, signer, false
}

// handleGuestbookForm shows the form to sign owner's guestbook, or says why
// the visitor can't.
func (s *Server) handleGuestbookForm(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.guestbookOwner(w, r)
	if !ok {
		return
	}
	data, _, _ := s.guestbookState(r, owner)
	s.renderTemplate(w, http.StatusOK, "guestbook_sign.html", data)
}

// handleGuestbookSign stores a new entry on owner's guestbook.
//
// Visitors who can't sign get the same explanation page as GET, with a
// status that fits: the sign-in and handle cases redirect, as POST /mood
// does, and signing your own book is a 403.
func (s *Server) handleGuestbookSign(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.guestbookOwner(w, r)
	if !ok {
		return
	}
	data, signer, canSign := s.guestbookState(r, owner)
	if !canSign {
		switch data.MessageURL {
		case "/login", "/":
			http.Redirect(w, r, data.MessageURL, http.StatusSeeOther)
		default:
			s.renderTemplate(w, http.StatusForbidden, "guestbook_sign.html", data)
		}
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	body := r.FormValue("body")

	if _, err := s.Guestbook.Sign(r.Context(), owner.ID, signer.ID, body); err != nil {
		switch {
		case errors.Is(err, guestbook.ErrInvalidBody):
			data.Body = body
			data.Error = "Escribí entre 1 y 280 caracteres."
			s.renderTemplate(w, http.StatusUnprocessableEntity, "guestbook_sign.html", data)
		case errors.Is(err, guestbook.ErrOwnPage):
			s.renderTemplate(w, http.StatusForbidden, "guestbook_sign.html", data)
		default:
			s.Logger.ErrorContext(r.Context(), "sign guestbook", slog.String("error", err.Error()))
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, data.PageURL, http.StatusSeeOther)
}
