package web_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/mayloo89/retratar/internal/config"
	"github.com/mayloo89/retratar/internal/session"
	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
	"github.com/mayloo89/retratar/internal/web"
)

// stubSender captures the last message sent instead of delivering it,
// so a test can pull the magic link straight out of it.
type stubSender struct {
	mu      sync.Mutex
	to      string
	subject string
	body    string
}

func (s *stubSender) Send(_ context.Context, to, subject, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.to, s.subject, s.body = to, subject, body
	return nil
}

// token pulls the login token out of the last message's link. It fails the
// test outright rather than returning an error: every caller needs a token to
// keep going, and a missing one means the request that should have triggered
// a send never got that far.
func (s *stubSender) token(t *testing.T, baseURL string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := baseURL + "/login/"
	_, after, found := strings.Cut(s.body, prefix)
	if !found {
		t.Fatalf("mail body has no login link: %q", s.body)
	}
	token, _, _ := strings.Cut(after, "\n")
	return strings.TrimSpace(token)
}

func newLoginServer(t *testing.T) (*web.Server, *stubSender) {
	t.Helper()
	pool := testdb.New(t)
	sender := &stubSender{}
	return &web.Server{
		Config: config.Config{
			Env:       config.EnvProduction,
			Addr:      ":8080",
			AppHost:   "retratar.com.ar",
			PagesHost: "retrat.ar",
		},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Users:    user.NewService(pool),
		Sessions: session.NewService(pool),
		Mailer:   sender,
	}, sender
}

// request is get from router_test.go plus a body and cookies, needed once the
// login flow requires POSTs and a nonce cookie carried between requests.
func request(t *testing.T, h http.Handler, method, host, path string, body io.Reader, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Host = host
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// completeLogin drives a full magic-link round trip for address: request a
// link, fetch the confirmation page for its nonce cookie, then post the
// confirmation. It returns the session cookie the last step issues.
func completeLogin(t *testing.T, h http.Handler, host string, sender *stubSender, baseURL, address string) *http.Cookie {
	t.Helper()

	request(t, h, http.MethodPost, host, "/login",
		strings.NewReader(url.Values{"email": {address}}.Encode())).Body.Close()

	token := sender.token(t, baseURL)

	confirm := request(t, h, http.MethodGet, host, "/login/"+token, nil)
	confirm.Body.Close()
	nonceCookie := findCookie(confirm, "__Host-login-nonce")
	if nonceCookie == nil {
		t.Fatal("GET /login/{token} did not set the nonce cookie")
	}

	form := url.Values{"nonce": {nonceCookie.Value}}
	complete := request(t, h, http.MethodPost, host, "/login/"+token,
		strings.NewReader(form.Encode()), nonceCookie)
	defer complete.Body.Close()

	sessionCookie := findCookie(complete, web.SessionCookieName)
	if sessionCookie == nil {
		t.Fatal("POST /login/{token} did not set the session cookie")
	}
	return sessionCookie
}

func TestLoginFlow_SignsInAndAuthenticatesFollowingRequests(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "ana@example.com")

	home := request(t, h, http.MethodGet, host, "/", nil, sessionCookie)
	defer home.Body.Close() //nolint:errcheck // httptest body close cannot fail
	body, err := io.ReadAll(home.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "ana@example.com") {
		t.Errorf("home body = %q, want it to mention the signed-in email", body)
	}
}

func TestLogout_RevokesTheSession(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, host, sender, srv.Config.BaseURL(), "cleo@example.com")

	logout := request(t, h, http.MethodPost, host, "/logout", nil, sessionCookie)
	logout.Body.Close() //nolint:errcheck // httptest body close cannot fail

	if _, err := srv.Sessions.Lookup(t.Context(), sessionCookie.Value); !errors.Is(err, session.ErrInvalidSession) {
		t.Fatalf("Sessions.Lookup() after logout error = %v, want ErrInvalidSession", err)
	}

	home := request(t, h, http.MethodGet, host, "/", nil, sessionCookie)
	defer home.Body.Close() //nolint:errcheck // httptest body close cannot fail
	body, _ := io.ReadAll(home.Body)
	if string(body) != "retratar app\n" {
		t.Errorf("home body after logout = %q, want the unauthenticated body", body)
	}
}

// TestLoginConfirm_DoesNotConsumeTheToken guards against the exact hazard the
// task calls out: a mail scanner or link prefetcher fetching the GET before
// the person it was sent to must not burn the link.
func TestLoginConfirm_DoesNotConsumeTheToken(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	request(t, h, http.MethodPost, host, "/login",
		strings.NewReader(url.Values{"email": {"deb@example.com"}}.Encode())).Body.Close()
	token := sender.token(t, srv.Config.BaseURL())

	var nonceCookie *http.Cookie
	for range 3 {
		resp := request(t, h, http.MethodGet, host, "/login/"+token, nil)
		resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
		nonceCookie = findCookie(resp, "__Host-login-nonce")
	}

	form := url.Values{"nonce": {nonceCookie.Value}}
	complete := request(t, h, http.MethodPost, host, "/login/"+token,
		strings.NewReader(form.Encode()), nonceCookie)
	defer complete.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if complete.StatusCode != http.StatusSeeOther {
		t.Fatalf("status after repeated GETs = %d, want %d", complete.StatusCode, http.StatusSeeOther)
	}
}

// TestLoginComplete_RejectsNonceMismatch is the login-CSRF guard: a POST
// carrying the right token but a nonce that does not match the cookie must
// not sign anyone in, and must not burn the token for the person it belongs
// to.
func TestLoginComplete_RejectsNonceMismatch(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	request(t, h, http.MethodPost, host, "/login",
		strings.NewReader(url.Values{"email": {"bea@example.com"}}.Encode())).Body.Close()
	token := sender.token(t, srv.Config.BaseURL())

	confirm := request(t, h, http.MethodGet, host, "/login/"+token, nil)
	confirm.Body.Close() //nolint:errcheck // httptest body close cannot fail
	nonceCookie := findCookie(confirm, "__Host-login-nonce")
	if nonceCookie == nil {
		t.Fatal("GET /login/{token} did not set the nonce cookie")
	}

	badForm := url.Values{"nonce": {"not-the-right-value"}}
	bad := request(t, h, http.MethodPost, host, "/login/"+token,
		strings.NewReader(badForm.Encode()), nonceCookie)
	bad.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if got := findCookie(bad, web.SessionCookieName); got != nil {
		t.Fatal("mismatched nonce still issued a session cookie")
	}

	goodForm := url.Values{"nonce": {nonceCookie.Value}}
	good := request(t, h, http.MethodPost, host, "/login/"+token,
		strings.NewReader(goodForm.Encode()), nonceCookie)
	defer good.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if findCookie(good, web.SessionCookieName) == nil {
		t.Fatal("legitimate completion after a rejected mismatch did not issue a session")
	}
}

// TestLoginRequest_ByteIdenticalForKnownAndUnknownEmail is the
// anti-enumeration requirement: requesting a link must look the same whether
// or not the address already has an account.
func TestLoginRequest_ByteIdenticalForKnownAndUnknownEmail(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	host := "retratar.com.ar"

	completeLogin(t, h, host, sender, srv.Config.BaseURL(), "known@example.com")

	knownResp := request(t, h, http.MethodPost, host, "/login",
		strings.NewReader(url.Values{"email": {"known@example.com"}}.Encode()))
	knownBody, err := io.ReadAll(knownResp.Body)
	knownResp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if err != nil {
		t.Fatalf("read known body: %v", err)
	}

	unknownResp := request(t, h, http.MethodPost, host, "/login",
		strings.NewReader(url.Values{"email": {"unknown@example.com"}}.Encode()))
	unknownBody, err := io.ReadAll(unknownResp.Body)
	unknownResp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if err != nil {
		t.Fatalf("read unknown body: %v", err)
	}

	if knownResp.StatusCode != unknownResp.StatusCode {
		t.Fatalf("status differs: known = %d, unknown = %d", knownResp.StatusCode, unknownResp.StatusCode)
	}
	if string(knownBody) != string(unknownBody) {
		t.Fatalf("body differs:\nknown   = %q\nunknown = %q", knownBody, unknownBody)
	}
}
