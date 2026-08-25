package web

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"

	"github.com/mayloo89/retratar/internal/session"
	"github.com/mayloo89/retratar/internal/user"
)

// loginFormData is what login_form.html renders. Error is empty on the first
// visit and set when a submission was rejected before a link was sent.
type loginFormData struct {
	Error string
}

// loginConfirmData is what login_confirm.html renders. Nonce must round-trip
// through the page's form field unchanged; see [handleLoginComplete].
type loginConfirmData struct {
	Token string
	Nonce string
}

func (s *Server) handleLoginForm(w http.ResponseWriter, _ *http.Request) {
	s.renderTemplate(w, http.StatusOK, "login_form.html", loginFormData{})
}

// handleLoginRequest mints a magic link and mails it.
//
// The response is the same shape whether or not the address belongs to an
// account: [user.Service.RequestLogin] never checks, so there is nothing here
// to special-case. A syntactically invalid address is rejected before that
// call and is not an enumeration signal — it says nothing about any account,
// only about the text typed into the box.
func (s *Server) handleLoginRequest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	address, err := user.NormaliseEmail(r.FormValue("email"))
	if err != nil {
		s.renderTemplate(w, http.StatusUnprocessableEntity, "login_form.html",
			loginFormData{Error: "Enter a valid email address."})
		return
	}

	raw, err := s.Users.RequestLogin(r.Context(), address)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "request login", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	link := s.Config.BaseURL() + "/login/" + raw
	body := "Click to sign in to retratar: " + link + "\n\nThis link expires in 15 minutes."
	if err := s.Mailer.Send(r.Context(), address, "Sign in to retratar", body); err != nil {
		s.Logger.ErrorContext(r.Context(), "send login mail", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, http.StatusOK, "login_check_email.html", nil)
}

// handleLoginConfirm serves the confirmation page a magic link points at. It
// deliberately never spends the token: a mail scanner or link prefetcher that
// fetches this URL ahead of the person it was sent to must not be able to
// burn it. Spending happens only in [handleLoginComplete], on the POST this
// page's own form sends.
func (s *Server) handleLoginConfirm(w http.ResponseWriter, r *http.Request) {
	nonce := newNonce()
	setLoginNonceCookie(w, nonce)
	s.renderTemplate(w, http.StatusOK, "login_confirm.html", loginConfirmData{
		Token: r.PathValue("token"),
		Nonce: nonce,
	})
}

// handleLoginComplete spends the token and, on success, signs the caller in.
//
// The nonce cookie set by [handleLoginConfirm] must match the form field the
// confirmation page echoed back. A cross-site page can make a browser POST
// here with the token from a link it does not control, but it was never
// handed the nonce cookie, so the values cannot agree; that mismatch is what
// stops a forged confirmation.
func (s *Server) handleLoginComplete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderLoginInvalid(w)
		return
	}

	cookie, err := r.Cookie(loginNonceCookieName)
	if err != nil || !nonceMatches(cookie.Value, r.FormValue("nonce")) {
		s.renderLoginInvalid(w)
		return
	}
	clearLoginNonceCookie(w)

	u, err := s.Users.CompleteLogin(r.Context(), r.PathValue("token"))
	if err != nil {
		if errors.Is(err, user.ErrInvalidToken) {
			s.renderLoginInvalid(w)
			return
		}
		s.Logger.ErrorContext(r.Context(), "complete login", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	sessionToken, err := s.Sessions.Issue(r.Context(), u.ID)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "issue session", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	SetSessionCookie(w, sessionToken, session.TTL)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleLogout revokes the current session, if any, and always clears the
// cookie. Revoking a token that is already invalid is not an error — see
// [session.Service.Revoke] — so logout looks like it worked whether or not
// the session was still live.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		if err := s.Sessions.Revoke(r.Context(), cookie.Value); err != nil {
			s.Logger.ErrorContext(r.Context(), "revoke session", slog.String("error", err.Error()))
		}
	}
	ClearSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) renderLoginInvalid(w http.ResponseWriter) {
	s.renderTemplate(w, http.StatusOK, "login_invalid.html", nil)
}

// renderTemplate executes a named template from [templates] into the
// response, logging rather than failing the request if it errors — by the
// time execution starts, headers are already written and there is no way to
// send a different response instead.
func (s *Server) renderTemplate(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		s.Logger.Error("render template", slog.String("template", name), slog.String("error", err.Error()))
	}
}

// nonceMatches compares a cookie value against a submitted form value in
// constant time. Both are unguessable random values, not attacker-known
// secrets compared many times over the network, so a timing side channel is
// not a realistic threat here — the constant-time compare simply costs
// nothing and matches how every other token in this program is compared.
func nonceMatches(cookie, form string) bool {
	return cookie != "" && subtle.ConstantTimeCompare([]byte(cookie), []byte(form)) == 1
}
