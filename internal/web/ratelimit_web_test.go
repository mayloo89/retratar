package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// requestFrom is request() from login_test.go with control over the peer
// address, so a test can act as several distinct clients against one handler.
func requestFrom(t *testing.T, h http.Handler, remoteAddr, method, host, path string, body io.Reader) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Host = host
	req.RemoteAddr = remoteAddr
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func loginForm() io.Reader {
	return strings.NewReader(url.Values{"email": {"someone@example.com"}}.Encode())
}

// TestLoginRateLimited_BlocksAfterBurstPerIP proves the limiter is actually
// wired to POST /login, that it starts returning 429 once an IP is over
// budget, and that the limit is per-IP: a second address is unaffected by the
// first one being throttled.
func TestLoginRateLimited_BlocksAfterBurstPerIP(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	const host = "retratar.com.ar"

	var last int
	for range 12 {
		resp := requestFrom(t, h, "203.0.113.10:5000", http.MethodPost, host, "/login", loginForm())
		last = resp.StatusCode
		resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after 12 rapid POST /login from one IP, status = %d, want 429", last)
	}

	other := requestFrom(t, h, "203.0.113.11:5000", http.MethodPost, host, "/login", loginForm())
	defer other.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if other.StatusCode == http.StatusTooManyRequests {
		t.Fatalf("a different IP got 429; the limit is global, not per-IP")
	}
}

// TestLoginRateLimited_Returns429WithoutLimitDetail checks the throttled
// response is a bare 429 that does not leak the limiter's rate or burst.
func TestLoginRateLimited_Returns429WithoutLimitDetail(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	const host = "retratar.com.ar"

	var resp *http.Response
	for range 12 {
		if resp != nil {
			resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
		}
		resp = requestFrom(t, h, "203.0.113.12:5000", http.MethodPost, host, "/login", loginForm())
	}
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, leak := range []string{"20s", "burst", "token", "rate=", "per minute", "Retry-After"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("429 body mentions %q; it should say nothing about the limit: %q", leak, body)
		}
	}
	if got := resp.Header.Get("Retry-After"); got != "" {
		t.Errorf("Retry-After = %q; a retry hint leaks the refill rate", got)
	}
}
