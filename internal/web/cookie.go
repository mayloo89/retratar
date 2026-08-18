package web

import (
	"net/http"
	"time"
)

// SessionCookieName carries the __Host- prefix deliberately.
//
// The prefix is enforced by the browser, not by us: a cookie whose name starts
// with __Host- is rejected unless it is Secure, has Path=/, and carries no
// Domain attribute. No Domain means host-only, so a subdomain cannot overwrite
// it. That single rule shuts down cookie tossing, where a page under a shared
// parent domain sets a Domain-scoped cookie and fixes the owner's session.
//
// Never remove the prefix to make local development easier. Browsers treat
// localhost as a secure context, so the prefix works over plain HTTP there.
const SessionCookieName = "__Host-session"

// SetSessionCookie writes the session cookie with the attributes the __Host-
// prefix requires. It intentionally takes no domain argument; there is no
// supported way to widen this cookie's scope.
func SetSessionCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie expires the session cookie. The attributes must match
// those used when setting it or the browser keeps the original.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
