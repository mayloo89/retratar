package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// RequestIDFrom returns the request ID attached by [RequestID], or "" if none.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// Middleware wraps a handler with behaviour that applies to every request.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so the first listed runs outermost.
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// RequestID attaches a random identifier to each request so a log line can be
// traced back to one visit.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf [8]byte
		rand.Read(buf[:]) //nolint:errcheck // crypto/rand.Read never returns an error
		ctx := context.WithValue(r.Context(), requestIDKey, hex.EncodeToString(buf[:]))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder captures the status code so RequestLogger can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// RequestLogger logs one line per request after it completes.
//
// The query string is never logged: magic-link tokens travel in it, and a log
// file is a place tokens must not be.
func RequestLogger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("host", r.Host),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int64("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// Recover turns a panic into a 500 so one bad request cannot take the process
// down. It logs the panic value; the client is told nothing.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//nolint:contextcheck // the closure logs with r.Context(); the
			// linter cannot follow a context through a deferred func literal.
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler { //nolint:errorlint // sentinel is panicked by value
					panic(v)
				}
				logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.String("request_id", RequestIDFrom(r.Context())),
					slog.Any("panic", v),
					slog.String("path", r.URL.Path),
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders sets response headers that are identical on both surfaces.
// The Content-Security-Policy differs per surface and is set by the surface's
// own middleware; see [AppCSP] and [PagesCSP].
func SecurityHeaders(production bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-site")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
			if production {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// appCSP governs the owner-facing surface. Scripts come from our own origin
// only; there is no CDN, so no third party can inject into the editor.
const appCSP = "default-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"form-action 'self'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'"

// pagesCSP governs rendered user pages, and is the tightest policy in the
// program.
//
// script-src is absent, so default-src 'none' denies every script: an inline
// handler, a <script> tag that survived sanitising, a javascript: URL. User
// customisation is CSS, and CSS alone.
//
// style-src is 'self' with no 'unsafe-inline'. That is a deliberate constraint
// on the renderer, not an oversight: theme CSS and per-page customisation must
// be served as stylesheets from our origin. The moment a style attribute is
// inlined into page HTML this policy breaks the page, which is the intended
// feedback. Do not relax it to 'unsafe-inline'.
const pagesCSP = "default-src 'none'; " +
	"style-src 'self'; " +
	"img-src 'self' https: data:; " +
	"font-src 'self'; " +
	"form-action 'self'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'; " +
	"sandbox allow-forms allow-same-origin"

// AppCSP applies the owner-facing Content-Security-Policy.
func AppCSP(next http.Handler) http.Handler {
	return csp(appCSP, next)
}

// PagesCSP applies the public-page Content-Security-Policy.
func PagesCSP(next http.Handler) http.Handler {
	return csp(pagesCSP, next)
}

func csp(policy string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", policy)
		next.ServeHTTP(w, r)
	})
}
