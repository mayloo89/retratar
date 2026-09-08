// Package web owns HTTP: routing, middleware, and the security headers that
// keep the two surfaces apart.
//
// The program serves two surfaces from one binary, split by hostname:
//
//	retratar.com.ar   the owner-facing app. Login, editor, settings.
//	*.retrat.ar       rendered public pages, one subdomain per handle.
//
// They are different registrable domains so the session cookie issued by the
// app is unreachable from any page. [config.Config.Validate] refuses to start
// the process if that stops being true.
package web

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/mayloo89/retratar/internal/config"
	"github.com/mayloo89/retratar/internal/mail"
	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/session"
	"github.com/mayloo89/retratar/internal/user"
)

// Server holds everything the HTTP layer needs. Dependencies are struct fields
// assigned in main; there is no container and no framework.
type Server struct {
	Config config.Config
	Logger *slog.Logger

	Users    *user.Service
	Sessions *session.Service
	Moods    *mood.Service
	Mailer   mail.Sender
}

// Handler builds the root handler: shared middleware, then a split by hostname
// into the two surfaces.
func (s *Server) Handler() http.Handler {
	// One limiter instance, shared by the write routes it wraps in appRoutes.
	// Handler is called once per process (see cmd/server), so this is not a
	// per-request or per-call allocation.
	writes := newRateLimiter(writeRateBurst, writeRateRefill, limiterIdleTTL)

	app := Chain(s.appRoutes(writes), AppCSP, PrivateCache, CurrentUser(s.Sessions, s.Users))
	pages := Chain(s.pageRoutes(), PagesCSP)

	root := s.hostSplit(app, pages)

	return Chain(root,
		RequestID,
		RequestLogger(s.Logger),
		Recover(s.Logger),
		SecurityHeaders(s.Config.IsProduction()),
	)
}

// hostSplit dispatches on the request's Host header.
//
// A request whose host matches neither surface gets 404, never a default. An
// unmatched host means a hostname points here that we did not configure, and
// serving the app to it would put the login form on an origin we do not
// control.
func (s *Server) hostSplit(app, pages http.Handler) http.Handler {
	appHost := canonicalHost(s.Config.AppHost)
	pagesHost := canonicalHost(s.Config.PagesHost)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := canonicalHost(r.Host)

		switch host {
		case appHost:
			app.ServeHTTP(w, r)
		case pagesHost:
			// The bare pages domain is not a page. Send visitors to the app.
			http.Redirect(w, r, s.Config.BaseURL()+"/", http.StatusFound)
		default:
			handle, ok := handleFromHost(host, pagesHost)
			if !ok {
				http.NotFound(w, r)
				return
			}
			pages.ServeHTTP(w, r.WithContext(withHandle(r.Context(), handle)))
		}
	})
}

// appRoutes serves the owner-facing surface.
//
// writes rate-limits the endpoints a script can abuse cheaply. POST /login is
// the one that matters — it sends mail — but POST /handle and POST /mood are
// unbounded writes too, so they share the limiter. POST /logout is left off:
// it sends nothing, its work is a single idempotent revoke, and a forged
// logout is a nuisance rather than a cost. GET routes are read-only and the
// login sub-steps (GET/POST /login/{token}) are already gated by the nonce
// cookie and a single-use token.
func (s *Server) appRoutes(writes *rateLimiter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /{$}", s.handleAppHome)
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.Handle("POST /login", writes.rateLimit(http.HandlerFunc(s.handleLoginRequest)))
	mux.HandleFunc("GET /login/{token}", s.handleLoginConfirm)
	mux.HandleFunc("POST /login/{token}", s.handleLoginComplete)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /handle", s.handleClaimForm)
	mux.Handle("POST /handle", writes.rateLimit(http.HandlerFunc(s.handleClaimSubmit)))
	mux.Handle("POST /mood", writes.rateLimit(http.HandlerFunc(s.handleMoodSubmit)))
	return mux
}

// pageRoutes serves rendered public pages. Every handler here reads the handle
// from the request context; see [HandleFrom].
func (s *Server) pageRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handlePage)
	mux.HandleFunc("GET /theme.css", s.handleTheme)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// canonicalHost lowercases a host and drops a default port so that
// "Retrat.AR:443" and "retrat.ar" compare equal.
func canonicalHost(hostport string) string {
	host := strings.ToLower(strings.TrimSpace(hostport))
	host = strings.TrimSuffix(host, ":443")
	host = strings.TrimSuffix(host, ":80")
	return host
}
