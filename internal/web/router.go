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
	app := Chain(s.appRoutes(), AppCSP, PrivateCache, CurrentUser(s.Sessions, s.Users))
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
func (s *Server) appRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /internal/tls-check", s.handleTLSCheck)
	mux.HandleFunc("GET /{$}", s.handleAppHome)
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLoginRequest)
	mux.HandleFunc("GET /login/{token}", s.handleLoginConfirm)
	mux.HandleFunc("POST /login/{token}", s.handleLoginComplete)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /handle", s.handleClaimForm)
	mux.HandleFunc("POST /handle", s.handleClaimSubmit)
	mux.HandleFunc("POST /mood", s.handleMoodSubmit)
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

// handleTLSCheck answers Caddy's on-demand TLS "ask" probe.
//
// Wildcard certificates for *.retrat.ar would need a DNS-01 challenge, so
// certificates are issued per hostname on first request instead. Without this
// check, anyone could point a hostname at the server and make it request a
// certificate, burning the certificate authority's rate limit for the whole
// domain. Caddy asks here first and only proceeds on 200.
//
// This route must not be reachable from the public internet; the reverse proxy
// calls it over loopback.
func (s *Server) handleTLSCheck(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		http.Error(w, "missing domain", http.StatusBadRequest)
		return
	}

	// A domain in an ACME request never carries a port, so compare against
	// port-less hosts. Locally the configured hosts do carry one.
	host := stripPort(canonicalHost(domain))
	appHost := stripPort(canonicalHost(s.Config.AppHost))
	pagesHost := stripPort(canonicalHost(s.Config.PagesHost))

	if host == appHost || host == pagesHost {
		w.WriteHeader(http.StatusOK)
		return
	}
	// TODO(phase-1): once handles are stored, also require that the handle is
	// registered. Shape-checking alone still allows a certificate per
	// well-formed name.
	if _, ok := handleFromHost(host, pagesHost); ok {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.Logger.WarnContext(r.Context(), "on-demand TLS refused", slog.String("domain", domain))
	http.Error(w, "unknown domain", http.StatusForbidden)
}

// stripPort removes any port from a host[:port] string. net.SplitHostPort is
// not used because it errors on a bare host.
func stripPort(hostport string) string {
	if i := strings.LastIndexByte(hostport, ':'); i >= 0 {
		return hostport[:i]
	}
	return hostport
}

// canonicalHost lowercases a host and drops a default port so that
// "Retrat.AR:443" and "retrat.ar" compare equal.
func canonicalHost(hostport string) string {
	host := strings.ToLower(strings.TrimSpace(hostport))
	host = strings.TrimSuffix(host, ":443")
	host = strings.TrimSuffix(host, ":80")
	return host
}
