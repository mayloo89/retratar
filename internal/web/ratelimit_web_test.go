package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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
// wired to POST /login, that an IP's opening requests are allowed and the
// block lands only once its budget is spent, and that the limit is per-IP: a
// second address is unaffected by the first one being throttled.
func TestLoginRateLimited_BlocksAfterBurstPerIP(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	const host = "retratar.com.ar"

	// 12 back-to-back requests from one IP, no time passing between them:
	// some prefix is allowed, the rest are 429, and once it flips it stays
	// flipped (no refill without elapsed time).
	statuses := make([]int, 12)
	for i := range statuses {
		resp := requestFrom(t, h, "203.0.113.10:5000", http.MethodPost, host, "/login", loginForm())
		statuses[i] = resp.StatusCode
		resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	}

	firstBlocked := slices.Index(statuses, http.StatusTooManyRequests)
	if firstBlocked < 1 {
		t.Fatalf("first 429 at index %d; a fresh IP's opening requests must be allowed, and 12 rapid ones must eventually block: %v", firstBlocked, statuses)
	}
	for i, s := range statuses {
		if i < firstBlocked && s == http.StatusTooManyRequests {
			t.Fatalf("request %d was blocked before the burst was spent: %v", i+1, statuses)
		}
		if i >= firstBlocked && s != http.StatusTooManyRequests {
			t.Fatalf("request %d was allowed after the limiter engaged (status %d): %v", i+1, s, statuses)
		}
	}

	other := requestFrom(t, h, "203.0.113.11:5000", http.MethodPost, host, "/login", loginForm())
	defer other.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if other.StatusCode == http.StatusTooManyRequests {
		t.Fatalf("a different IP got 429 while the first was throttled; the limit is global, not per-IP")
	}
}

// TestLoginRateLimited_Returns429WithoutLimitDetail checks the throttled
// response body is exactly "rate limited\n" and carries no Retry-After. Both
// together mean the response reveals neither the rate, the burst, nor the
// caller's remaining budget.
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got := string(body); got != "rate limited\n" {
		t.Errorf("429 body = %q, want exactly %q (nothing about rate, burst or remaining budget)", got, "rate limited\n")
	}
	if got := resp.Header.Get("Retry-After"); got != "" {
		t.Errorf("Retry-After = %q, want none: it would reveal the refill rate", got)
	}
}
