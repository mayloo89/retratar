package web

import (
	"crypto/rand"
	"net/http"
	"time"
)

// loginNonceCookieName carries the __Host- prefix for the same reason
// SessionCookieName does: the browser refuses it unless it is Secure, has
// Path=/, and carries no Domain, so no subdomain can read or overwrite it.
const loginNonceCookieName = "__Host-login-nonce"

// loginNonceTTL only has to survive between the GET that serves the
// confirmation page and the POST a click on its button sends a moment later.
// It matches user.TokenTTL so the login flow has one expiry to reason about,
// not two.
const loginNonceTTL = 15 * time.Minute

// newNonce returns a fresh value to bind a login confirmation page to the POST
// it will send, using the same generator as every other token in this program.
func newNonce() string {
	return rand.Text()
}

// setLoginNonceCookie writes the value a POST to /login/{token} must echo back
// in its form. Binding the two requests is what stops a cross-site form —
// which never received this cookie — from forging the POST that a bare
// GET-triggers-POST link would otherwise allow.
func setLoginNonceCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     loginNonceCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(loginNonceTTL.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearLoginNonceCookie expires the nonce cookie once the confirmation POST
// has been handled, whichever way it went.
func clearLoginNonceCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     loginNonceCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
