package web_test

import (
	"bytes"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestHandleOGImage_RendersMoodForAClaimedHandle(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	appHost := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, appHost, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, appHost, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()
	request(t, h, http.MethodPost, appHost, "/mood",
		strings.NewReader(url.Values{"mood_key": {"inspirado"}, "note": {"nuevo proyecto"}}.Encode()),
		sessionCookie).Body.Close()

	resp := request(t, h, http.MethodGet, "ana.retrat.ar", "/og.png", nil)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 1200 || b.Dy() != 630 {
		t.Errorf("dimensions = %dx%d, want 1200x630", b.Dx(), b.Dy())
	}
}

func TestHandleOGImage_NoMoodYetStillRenders200(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	appHost := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, appHost, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, appHost, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()

	resp := request(t, h, http.MethodGet, "ana.retrat.ar", "/og.png", nil)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (empty-state card, not an error)", resp.StatusCode)
	}
}

func TestHandleOGImage_UnclaimedHandleIs404(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()

	resp := request(t, h, http.MethodGet, "nobody.retrat.ar", "/og.png", nil)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestHandleOGImage_CacheHeadersAndConditionalGet checks the caching
// contract that keeps a scraper or a repeat paste from re-rendering: a
// public max-age, a stable ETag for the same mood, and a 304 (with no body)
// on a matching If-None-Match.
func TestHandleOGImage_CacheHeadersAndConditionalGet(t *testing.T) {
	srv, sender := newLoginServer(t)
	h := srv.Handler()
	appHost := "retratar.com.ar"

	sessionCookie := completeLogin(t, h, appHost, sender, srv.Config.BaseURL(), "ana@example.com")
	request(t, h, http.MethodPost, appHost, "/handle",
		strings.NewReader(url.Values{"handle": {"ana"}}.Encode()), sessionCookie).Body.Close()
	request(t, h, http.MethodPost, appHost, "/mood",
		strings.NewReader(url.Values{"mood_key": {"feliz"}, "note": {""}}.Encode()), sessionCookie).Body.Close()

	first := request(t, h, http.MethodGet, "ana.retrat.ar", "/og.png", nil)
	first.Body.Close() //nolint:errcheck // httptest body close cannot fail

	if got := first.Header.Get("Cache-Control"); got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want %q", got, "public, max-age=300")
	}
	etag := first.Header.Get("ETag")
	if etag == "" {
		t.Fatal("ETag is empty")
	}

	req := requestWithHeaders(t, h, "ana.retrat.ar", "/og.png", map[string]string{"If-None-Match": etag})
	defer req.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if req.StatusCode != http.StatusNotModified {
		t.Fatalf("status with matching If-None-Match = %d, want 304", req.StatusCode)
	}
	if body, _ := io.ReadAll(req.Body); len(body) != 0 {
		t.Errorf("304 response had a %d-byte body, want empty", len(body))
	}

	// Changing the mood must change the ETag — a stale cache must not keep
	// serving the old mood's card.
	request(t, h, http.MethodPost, appHost, "/mood",
		strings.NewReader(url.Values{"mood_key": {"triste"}, "note": {""}}.Encode()), sessionCookie).Body.Close()
	second := request(t, h, http.MethodGet, "ana.retrat.ar", "/og.png", nil)
	second.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if got := second.Header.Get("ETag"); got == etag {
		t.Error("ETag did not change after the mood changed")
	}
}

func requestWithHeaders(t *testing.T, h http.Handler, host, path string, headers map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

// TestOGImageRateLimited_BlocksAfterBurstPerIP proves reads is actually
// wired to GET /og.png, with its own, looser budget than the write routes'
// limiter (see ogRateBurst in ratelimit.go).
func TestOGImageRateLimited_BlocksAfterBurstPerIP(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()

	// ogRateBurst is 30; a handle that 404s is the cheapest way to spend a
	// client's whole budget without a real render on every call.
	const n = 34
	statuses := make([]int, n)
	for i := range statuses {
		resp := requestFrom(t, h, "203.0.113.60:5000", http.MethodGet, "nobody.retrat.ar", "/og.png", nil)
		statuses[i] = resp.StatusCode
		resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	}

	firstBlocked := slices.Index(statuses, http.StatusTooManyRequests)
	if firstBlocked < 1 {
		t.Fatalf("first 429 at index %d; a fresh IP's opening requests must be allowed, and %d rapid ones must eventually block: %v", firstBlocked, n, statuses)
	}
}
