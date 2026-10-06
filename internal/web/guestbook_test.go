package web_test

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/guestbook"
	"github.com/mayloo89/retratar/internal/user"
	"github.com/mayloo89/retratar/internal/web"
)

const guestbookAppHost = "retratar.com.ar"

// guestbookAccount creates an account straight through the services, with a
// session cookie, and claims handle unless it is empty. Going around the HTTP
// login keeps these tests off the write rate limiter, which one client
// address would otherwise exhaust long before the test reaches its point.
func guestbookAccount(t *testing.T, srv *web.Server, email, handle string) (*http.Cookie, user.User) {
	t.Helper()
	raw, err := srv.Users.RequestLogin(t.Context(), email)
	if err != nil {
		t.Fatalf("RequestLogin() error = %v", err)
	}
	u, err := srv.Users.CompleteLogin(t.Context(), raw)
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v", err)
	}
	if handle != "" {
		if u, err = srv.Users.ClaimHandle(t.Context(), u.ID, handle); err != nil {
			t.Fatalf("ClaimHandle() error = %v", err)
		}
	}
	token, err := srv.Sessions.Issue(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("Sessions.Issue() error = %v", err)
	}
	return &http.Cookie{Name: web.SessionCookieName, Value: token}, u
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// pageBody fetches a public page and returns its body.
func pageBody(t *testing.T, h http.Handler, host string) string {
	t.Helper()
	resp := request(t, h, http.MethodGet, host, "/", nil)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func signForm(body string) io.Reader {
	return strings.NewReader(url.Values{"body": {body}}.Encode())
}

func TestPage_ShowsGuestbookEntries(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	guestbookAccount(t, srv, "a@example.com", "a")
	bCookie, _ := guestbookAccount(t, srv, "b@example.com", "b")

	request(t, h, http.MethodPost, guestbookAppHost, "/firmar/a", signForm("hola desde b"), bCookie).Body.Close()

	body := pageBody(t, h, "a.retrat.ar")
	for _, want := range []string{
		"hola desde b",
		`<a href="https://b.retrat.ar/">b</a>`,
		`href="https://retratar.com.ar/firmar/a"`,
		"Firmá el libro de a",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page body = %q, want it to contain %q", body, want)
		}
	}
}

func TestPage_GuestbookEscapesBody(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	_, a := guestbookAccount(t, srv, "a@example.com", "a")
	_, b := guestbookAccount(t, srv, "b@example.com", "b")
	if _, err := srv.Guestbook.Sign(t.Context(), a.ID, b.ID, "<script>alert(1)</script>"); err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	body := pageBody(t, h, "a.retrat.ar")
	if strings.Contains(body, "<script>") {
		t.Errorf("page body contains a literal <script>: %q", body)
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("page body = %q, want the escaped script text", body)
	}
}

func TestPage_EmptyGuestbook(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	guestbookAccount(t, srv, "a@example.com", "a")

	body := pageBody(t, h, "a.retrat.ar")
	if !strings.Contains(body, "Todavía nadie firmó.") {
		t.Errorf("page body = %q, want the empty guestbook text", body)
	}
}

func TestGuestbookForm_States(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	aCookie, _ := guestbookAccount(t, srv, "a@example.com", "a")
	bCookie, _ := guestbookAccount(t, srv, "b@example.com", "b")
	noHandle, _ := guestbookAccount(t, srv, "c@example.com", "")

	tests := []struct {
		name   string
		path   string
		cookie *http.Cookie
		status int
		want   string
	}{
		{"not signed in", "/firmar/a", nil, http.StatusOK, "Ingresá para firmar el libro de a."},
		{"no handle", "/firmar/a", noHandle, http.StatusOK, "Elegí tu nombre de usuario para firmar."},
		{"own page", "/firmar/a", aCookie, http.StatusOK, "No podés firmar tu propio libro."},
		{"valid signer", "/firmar/a", bCookie, http.StatusOK, `<textarea id="body" name="body" maxlength="280"`},
		{"handle is case-insensitive", "/firmar/A", bCookie, http.StatusOK, `action="/firmar/a"`},
		{"unknown handle", "/firmar/nadie", bCookie, http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tt.cookie != nil {
				cookies = append(cookies, tt.cookie)
			}
			resp := request(t, h, http.MethodGet, guestbookAppHost, tt.path, nil, cookies...)
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
			}
			if body := readBody(t, resp); !strings.Contains(body, tt.want) {
				t.Errorf("body = %q, want it to contain %q", body, tt.want)
			}
		})
	}
}

func TestGuestbookSign_RedirectsToPage(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	_, a := guestbookAccount(t, srv, "a@example.com", "a")
	bCookie, _ := guestbookAccount(t, srv, "b@example.com", "b")

	resp := request(t, h, http.MethodPost, guestbookAppHost, "/firmar/a", signForm("  hola  "), bCookie)
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://a.retrat.ar/" {
		t.Errorf("Location = %q, want https://a.retrat.ar/", got)
	}
	entries, err := srv.Guestbook.Visible(t.Context(), a.ID, guestbook.PageLimit)
	if err != nil {
		t.Fatalf("Visible() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Body != "hola" || entries[0].AuthorHandle != "b" {
		t.Errorf("entries = %+v, want one trimmed entry by b", entries)
	}
}

func TestGuestbookSign_Rejects(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	aCookie, a := guestbookAccount(t, srv, "a@example.com", "a")
	bCookie, _ := guestbookAccount(t, srv, "b@example.com", "b")
	noHandle, _ := guestbookAccount(t, srv, "c@example.com", "")

	tests := []struct {
		name     string
		cookie   *http.Cookie
		body     string
		raw      string // a hand-built form body, for bytes url.Values would re-encode
		status   int
		location string
		want     string
	}{
		{"empty body", bCookie, "", "", http.StatusUnprocessableEntity, "", "Escribí entre 1 y 280 caracteres."},
		{"281 characters", bCookie, strings.Repeat("a", 281), "", http.StatusUnprocessableEntity, "", "Escribí entre 1 y 280 caracteres."},
		{"invalid UTF-8", bCookie, "", "body=ok%FFok", http.StatusUnprocessableEntity, "", "Escribí entre 1 y 280 caracteres."},
		{"own page", aCookie, "hola", "", http.StatusForbidden, "", "No podés firmar tu propio libro."},
		{"not signed in", nil, "hola", "", http.StatusSeeOther, "/login", ""},
		{"no handle", noHandle, "hola", "", http.StatusSeeOther, "/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tt.cookie != nil {
				cookies = append(cookies, tt.cookie)
			}
			form := signForm(tt.body)
			if tt.raw != "" {
				form = strings.NewReader(tt.raw)
			}
			resp := request(t, h, http.MethodPost, guestbookAppHost, "/firmar/a", form, cookies...)
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
			}
			if got := resp.Header.Get("Location"); got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			if body := readBody(t, resp); !strings.Contains(body, tt.want) {
				t.Errorf("body = %q, want it to contain %q", body, tt.want)
			}
			entries, err := srv.Guestbook.Visible(t.Context(), a.ID, guestbook.PageLimit)
			if err != nil {
				t.Fatalf("Visible() error = %v", err)
			}
			if len(entries) != 0 {
				t.Errorf("%d entries stored, want 0", len(entries))
			}
		})
	}
}

func TestGuestbookSign_InvalidBodyKeepsText(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	guestbookAccount(t, srv, "a@example.com", "a")
	bCookie, _ := guestbookAccount(t, srv, "b@example.com", "b")

	text := strings.Repeat("ñ", 281)
	resp := request(t, h, http.MethodPost, guestbookAppHost, "/firmar/a", signForm(text), bCookie)
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	body := readBody(t, resp)
	if !strings.Contains(body, text) {
		t.Errorf("body does not preserve the rejected text")
	}
}

func TestGuestbookSign_CrossOriginRefused(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	_, a := guestbookAccount(t, srv, "a@example.com", "a")
	bCookie, _ := guestbookAccount(t, srv, "b@example.com", "b")

	resp := requestWithBodyHeaders(t, h, map[string]string{"Sec-Fetch-Site": "cross-site"},
		http.MethodPost, guestbookAppHost, "/firmar/a", signForm("hola"), bCookie)
	resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	entries, err := srv.Guestbook.Visible(t.Context(), a.ID, guestbook.PageLimit)
	if err != nil {
		t.Fatalf("Visible() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("%d entries stored, want 0", len(entries))
	}
}

// TestGuestbookSign_RateLimited proves the route is wrapped by the writes
// limiter: rapid requests from one address eventually get 429, and once the
// limiter engages it stays engaged.
func TestGuestbookSign_RateLimited(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()
	guestbookAccount(t, srv, "a@example.com", "a")

	statuses := make([]int, 12)
	for i := range statuses {
		resp := requestFrom(t, h, "203.0.113.10:5000", http.MethodPost, guestbookAppHost, "/firmar/a", signForm("hola"))
		statuses[i] = resp.StatusCode
		resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	}

	firstBlocked := slices.Index(statuses, http.StatusTooManyRequests)
	if firstBlocked < 1 {
		t.Fatalf("first 429 at index %d; opening requests must pass and 12 rapid ones must block: %v", firstBlocked, statuses)
	}
	for i, s := range statuses {
		if (i < firstBlocked) == (s == http.StatusTooManyRequests) {
			t.Fatalf("request %d has status %d around the first block at %d: %v", i+1, s, firstBlocked, statuses)
		}
	}
}
