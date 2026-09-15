package web_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/config"
	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
	"github.com/mayloo89/retratar/internal/web"
)

func testServer(t *testing.T) *web.Server {
	t.Helper()
	pool := testdb.New(t)
	return &web.Server{
		Config: config.Config{
			Env:       config.EnvProduction,
			Addr:      ":8080",
			AppHost:   "retratar.com.ar",
			PagesHost: "retrat.ar",
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Users:  user.NewService(pool),
		Moods:  mood.NewService(pool),
	}
}

func get(t *testing.T, h http.Handler, host, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func TestHostSplit(t *testing.T) {
	h := testServer(t).Handler()

	tests := []struct {
		name         string
		host         string
		wantStatus   int
		wantBody     string
		wantLocation string
	}{
		// Anonymous requests to the app surface are sent to sign in; see
		// handleAppHome. That is a 303, not a 200, but the redirect target
		// still proves the host routed to the app surface and not somewhere
		// else.
		{"app surface", "retratar.com.ar", http.StatusSeeOther, "", "/login"},
		{"app surface ignores case", "RETRATAR.com.AR", http.StatusSeeOther, "", "/login"},
		{"app surface ignores default port", "retratar.com.ar:443", http.StatusSeeOther, "", "/login"},
		// testServer has no account with this handle claimed, so a
		// well-formed handle 404s here for the same reason an unclaimed one
		// would in production — this table is about routing, not content;
		// see page_test.go for a real account's page rendering.
		{"page", "sebas.retrat.ar", http.StatusNotFound, "", ""},
		{"bare pages domain redirects to app", "retrat.ar", http.StatusFound, "", "https://retratar.com.ar/"},
		// An unknown host must never fall through to a surface. A hostname
		// pointed at this server that we did not configure is not ours, and
		// serving the login form on it would put credentials on a foreign origin.
		{"unknown host", "evil.example.com", http.StatusNotFound, "", ""},
		{"nested page host", "a.b.retrat.ar", http.StatusNotFound, "", ""},
		{"subdomain of app host", "anything.retratar.com.ar", http.StatusNotFound, "", ""},
		{"reserved handle", "admin.retrat.ar", http.StatusNotFound, "", ""},
		// DNS is case-insensitive, so an uppercase host resolves to the same
		// (here, still unclaimed) handle.
		{"uppercase page host", "SEBAS.retrat.ar", http.StatusNotFound, "", ""},
		{"underscore in handle", "se_bas.retrat.ar", http.StatusNotFound, "", ""},
		{"leading hyphen in handle", "-sebas.retrat.ar", http.StatusNotFound, "", ""},
		{"punycode-shaped handle", "xn--a.retrat.ar", http.StatusNotFound, "", ""},
		{"empty host", "", http.StatusNotFound, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := get(t, h, tt.host, "/")
			defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.wantLocation != "" {
				if got := resp.Header.Get("Location"); got != tt.wantLocation {
					t.Errorf("Location = %q, want %q", got, tt.wantLocation)
				}
			}
			if tt.wantBody == "" {
				return
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

// TestPagesSurfaceNeverSetsCookies is the constraint the whole two-domain
// design exists to guarantee. If a response from a page host ever carries a
// Set-Cookie, session state has leaked onto an origin that renders
// user-controlled content, and account takeover follows.
func TestPagesSurfaceNeverSetsCookies(t *testing.T) {
	h := testServer(t).Handler()

	resp := get(t, h, "sebas.retrat.ar", "/")
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail

	if cookies := resp.Cookies(); len(cookies) != 0 {
		t.Fatalf("page response set %d cookie(s), want 0: %v", len(cookies), cookies)
	}
}

// TestPagesCSPDeniesScripts guards the second half of the same boundary. User
// pages are CSS, never scripts.
func TestPagesCSPDeniesScripts(t *testing.T) {
	h := testServer(t).Handler()

	resp := get(t, h, "sebas.retrat.ar", "/")
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail

	policy := resp.Header.Get("Content-Security-Policy")
	if policy == "" {
		t.Fatal("page response has no Content-Security-Policy")
	}
	for _, banned := range []string{"script-src", "unsafe-inline", "unsafe-eval"} {
		if strings.Contains(policy, banned) {
			t.Errorf("pages CSP contains %q, want it absent: %s", banned, policy)
		}
	}
	if !strings.Contains(policy, "default-src 'none'") {
		t.Errorf("pages CSP lacks default-src 'none': %s", policy)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := testServer(t).Handler()

	resp := get(t, h, "retratar.com.ar", "/")
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail

	want := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains; preload",
	}
	for header, value := range want {
		if got := resp.Header.Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
}

func TestHSTSOnlyInProduction(t *testing.T) {
	s := testServer(t)
	s.Config.Env = config.EnvDevelopment

	resp := get(t, s.Handler(), "retratar.com.ar", "/")
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail

	if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q in development, want empty", got)
	}
}

// TestAppSurfaceIsPrivateCache guards against a caching proxy serving one
// owner's authenticated page (see handleAppHome) to a different visitor.
func TestAppSurfaceIsPrivateCache(t *testing.T) {
	h := testServer(t).Handler()

	resp := get(t, h, "retratar.com.ar", "/")
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail

	if got := resp.Header.Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "private, no-store")
	}
	if got := resp.Header.Values("Vary"); !slices.Contains(got, "Cookie") {
		t.Errorf("Vary = %v, want it to contain %q", got, "Cookie")
	}
}

// TestPagesSurfaceHasNoPrivateCacheHeaders documents that the caching
// constraint is app-only: rendered public pages have no per-visitor state and
// are fine to cache.
func TestPagesSurfaceHasNoPrivateCacheHeaders(t *testing.T) {
	h := testServer(t).Handler()

	resp := get(t, h, "sebas.retrat.ar", "/")
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail

	if got := resp.Header.Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control = %q, want empty", got)
	}
}

func TestHealthz(t *testing.T) {
	resp := get(t, testServer(t).Handler(), "retratar.com.ar", "/healthz")
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
