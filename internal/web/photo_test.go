package web_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/config"
	"github.com/mayloo89/retratar/internal/guestbook"
	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/photo"
	"github.com/mayloo89/retratar/internal/photostore"
	"github.com/mayloo89/retratar/internal/session"
	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
	"github.com/mayloo89/retratar/internal/web"
)

const (
	appHost  = "retratar.com.ar"
	anaHost  = "ana.retrat.ar"
	bobHost  = "bob.retrat.ar"
	boxTitle = "Tu foto"

	msgToday     = "Ya cambiaste tu foto hoy. Mañana podés subir otra."
	msgNone      = "Elegí una foto."
	msgUnreadble = "No pudimos leer esa foto. Probá con un JPG o PNG."
	msgTooLarge  = "Esta foto es muy grande."
	msgBusy      = "Estamos procesando otra foto."
)

var art = time.FixedZone("ART", -3*60*60)

// clock is a settable time source for Server.Now.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

var remoteSeq atomic.Uint32

// nextAddr gives every test request its own client address, so the write
// limiter (burst 10 per IP) does not turn a long test into 429s.
func nextAddr() string {
	n := remoteSeq.Add(1)
	return "198.51." + strconv.Itoa(int(n>>8&0xff)) + "." + strconv.Itoa(int(n&0xff)) + ":4000"
}

type photoEnv struct {
	srv   *web.Server
	h     http.Handler
	pool  *pgxpool.Pool
	dir   string
	clock *clock
	send  *stubSender
}

// newPhotoEnv is newLoginServer with a photo store in a temp dir and a fixed
// clock at noon, Argentina time, on 2026-10-20.
func newPhotoEnv(t *testing.T) *photoEnv {
	t.Helper()
	pool := testdb.New(t)
	sender := &stubSender{}
	dir := t.TempDir()
	store, err := photostore.New(dir)
	if err != nil {
		t.Fatalf("photostore.New() error = %v", err)
	}
	clk := &clock{t: time.Date(2026, 10, 20, 12, 0, 0, 0, art)}
	srv := &web.Server{
		Config: config.Config{
			Env:       config.EnvProduction,
			Addr:      ":8080",
			AppHost:   appHost,
			PagesHost: "retrat.ar",
		},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Users:     user.NewService(pool),
		Sessions:  session.NewService(pool),
		Moods:     mood.NewService(pool),
		Guestbook: guestbook.NewService(pool),
		Mailer:    sender,
		Photos:    store,
		Now:       clk.Now,
	}
	return &photoEnv{srv: srv, h: srv.Handler(), pool: pool, dir: dir, clock: clk, send: sender}
}

// signUp signs in address, claims handle, and optionally allowlists the
// account for photo uploads. It returns the session cookie.
func (e *photoEnv) signUp(t *testing.T, address, handle string, allow bool) *http.Cookie {
	t.Helper()
	c := completeLogin(t, e.h, appHost, e.send, e.srv.Config.BaseURL(), address)
	request(t, e.h, http.MethodPost, appHost, "/handle",
		strings.NewReader(url.Values{"handle": {handle}}.Encode()), c).Body.Close()
	if allow {
		if _, err := e.pool.Exec(t.Context(),
			`UPDATE users SET can_upload_photos = true WHERE handle = $1`, handle); err != nil {
			t.Fatalf("allowlist: %v", err)
		}
	}
	return c
}

// files lists every entry in the photo directory, temporary files included.
func (e *photoEnv) files(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, ent := range entries {
		names = append(names, ent.Name())
	}
	return names
}

// upload posts file as the form's "photo" part.
func (e *photoEnv) upload(t *testing.T, c *http.Cookie, file []byte, hdr ...string) reply {
	t.Helper()
	body, ctype := multipartBody(t, "photo", "photo.jpg", file)
	return e.post(t, c, "/photo", ctype, body, hdr...)
}

func (e *photoEnv) post(t *testing.T, c *http.Cookie, path, ctype string, body io.Reader, hdr ...string) reply {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Host = appHost
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	if c != nil {
		req.AddCookie(c)
	}
	return e.do(req)
}

// reply is a response whose body has been read and closed.
type reply struct {
	Code    int
	Header  http.Header
	Body    []byte
	Cookies []*http.Cookie
}

// do serves req from its own client address, so the write limiter (burst 10
// per IP) does not turn a long test into 429s.
func (e *photoEnv) do(req *http.Request) reply {
	req.RemoteAddr = nextAddr()
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	resp := rec.Result()
	defer resp.Body.Close() //nolint:errcheck // httptest body close cannot fail
	b, _ := io.ReadAll(resp.Body)
	return reply{Code: resp.StatusCode, Header: resp.Header, Body: b, Cookies: resp.Cookies()}
}

func (e *photoEnv) home(t *testing.T, c *http.Cookie) string {
	t.Helper()
	r := e.status(t, appHost, "/", c)
	wantStatus(t, r, http.StatusOK)
	return string(r.Body)
}

func (e *photoEnv) status(t *testing.T, host, path string, cookies ...*http.Cookie) reply {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return e.do(req)
}

func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if data != nil {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="`+field+`"; filename="`+filename+`"`)
		h.Set("Content-Type", "application/octet-stream")
		part, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.WriteField("other", "x"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 9), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// jpegWithEXIF inserts an APP1 (Exif) segment right after SOI.
func jpegWithEXIF(t *testing.T, w, h int) []byte {
	t.Helper()
	jpg := testJPEG(t, w, h)
	payload := append([]byte("Exif\x00\x00"), []byte("GPS-1234567890-secret")...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

// hasAPP1 walks the JPEG's segments up to the scan and reports an APP1.
func hasAPP1(t *testing.T, jpg []byte) bool {
	t.Helper()
	if len(jpg) < 4 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		t.Fatal("not a JPEG: no SOI")
	}
	i := 2
	for i+4 <= len(jpg) {
		if jpg[i] != 0xFF {
			t.Fatalf("bad marker byte at %d", i)
		}
		m := jpg[i+1]
		if m == 0xE1 {
			return true
		}
		if m == 0xDA { // start of scan: headers are over
			return false
		}
		i += 2 + int(binary.BigEndian.Uint16(jpg[i+2:]))
	}
	return false
}

func testPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// bombPNG declares w x h pixels in a valid header with nothing behind it.
func bombPNG(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6
	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	out = append(out, chunk...)
	out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
	return append(out, bytes.Repeat([]byte{0xAB}, 64)...)
}

var photoSrc = regexp.MustCompile(`/photo/([a-z2-7]{26})\.jpg`)

// currentKey reads the photo key out of the owner's dashboard.
func (e *photoEnv) currentKey(t *testing.T, c *http.Cookie) string {
	t.Helper()
	m := photoSrc.FindStringSubmatch(e.home(t, c))
	if m == nil {
		t.Fatal("dashboard shows no photo")
	}
	return m[1]
}

func wantStatus(t *testing.T, r reply, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("status = %d, want %d; body: %.200s", r.Code, want, r.Body)
	}
}

func wantBodyHas(t *testing.T, r reply, want int, text string) {
	t.Helper()
	wantStatus(t, r, want)
	if !strings.Contains(string(r.Body), text) {
		t.Fatalf("body does not contain %q; body: %.600s", text, r.Body)
	}
}

func TestPhotoUpload_HappyPath(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	wantStatus(t, e.upload(t, c, jpegWithEXIF(t, 60, 30)), http.StatusSeeOther)

	home := e.home(t, c)
	if !strings.Contains(home, boxTitle) {
		t.Error("dashboard has no Tu foto box")
	}
	key := e.currentKey(t, c)
	for _, want := range []string{`width="60"`, `height="30"`, `alt="Tu foto actual"`, `action="/photo/remove"`} {
		if !strings.Contains(home, want) {
			t.Errorf("dashboard does not contain %q", want)
		}
	}

	resp := e.status(t, anaHost, "/photo/"+key+".jpg")
	if resp.Code != http.StatusOK {
		t.Fatalf("GET page-host photo status = %d, want 200", resp.Code)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want public immutable", got)
	}
	if resp.Header.Get("Last-Modified") == "" {
		t.Error("Last-Modified is empty, want ServeContent to set it")
	}
	if hasAPP1(t, resp.Body) {
		t.Error("served JPEG has an APP1 (Exif) segment")
	}
	if bytes.Contains(resp.Body, []byte("GPS-1234567890-secret")) {
		t.Error("served bytes still carry the Exif payload")
	}
	img, err := jpeg.Decode(bytes.NewReader(resp.Body))
	if err != nil {
		t.Fatalf("served bytes do not decode as JPEG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 60 || b.Dy() != 30 {
		t.Errorf("served size = %dx%d, want 60x30", b.Dx(), b.Dy())
	}
}

func TestPhotoUpload_Replace(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)
	oldKey := e.currentKey(t, c)

	e.clock.Set(e.clock.Now().Add(24 * time.Hour))
	wantStatus(t, e.upload(t, c, testJPEG(t, 50, 25)), http.StatusSeeOther)
	newKey := e.currentKey(t, c)

	if newKey == oldKey {
		t.Fatal("key did not change on replace")
	}
	if _, err := os.Stat(filepath.Join(e.dir, oldKey+".jpg")); !os.IsNotExist(err) {
		t.Errorf("old file still on disk (stat error = %v)", err)
	}
	if resp := e.status(t, anaHost, "/photo/"+oldKey+".jpg"); resp.Code != http.StatusNotFound {
		t.Errorf("old URL status = %d, want 404", resp.Code)
	}
	if resp := e.status(t, anaHost, "/photo/"+newKey+".jpg"); resp.Code != http.StatusOK {
		t.Errorf("new URL status = %d, want 200", resp.Code)
	}
	if got := e.files(t); len(got) != 1 {
		t.Errorf("store has %v, want exactly the new file", got)
	}
}

func TestPhotoUpload_OncePerDay(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)
	key := e.currentKey(t, c)

	wantBodyHas(t, e.upload(t, c, testJPEG(t, 44, 22)), http.StatusUnprocessableEntity, msgToday)
	if got := e.currentKey(t, c); got != key {
		t.Errorf("stored key changed to %q, want %q", got, key)
	}
	home := e.home(t, c)
	if strings.Contains(home, `action="/photo" enctype`) {
		t.Error("dashboard still offers the upload form after today's upload")
	}
	if !strings.Contains(home, msgToday) {
		t.Error("dashboard does not explain the daily limit")
	}
	if got := e.files(t); len(got) != 1 {
		t.Errorf("store has %v, want only the first photo", got)
	}
}

func TestPhotoUpload_RaceLoserIsRejectedAndCleanedUp(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	// Both requests pass the early check (the dashboard has no photo yet)
	// and only SetPhoto decides.
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := e.upload(t, c, testJPEG(t, 40, 20))
			codes[i] = resp.Code
		}()
	}
	wg.Wait()

	wins, losses := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusSeeOther:
			wins++
		case http.StatusUnprocessableEntity:
			losses++
		default:
			t.Errorf("status = %d, want 303 or 422", code)
		}
	}
	if wins != 1 || losses != 1 {
		t.Errorf("wins = %d, losses = %d, want 1 and 1", wins, losses)
	}
	if got := e.files(t); len(got) != 1 {
		t.Errorf("store has %v, want exactly the winner's file", got)
	}
}

func TestPhotoUpload_DayBoundaryIsArgentina(t *testing.T) {
	tests := []struct {
		name        string
		first, next time.Time
		wantAllowed bool
	}{
		// 23:30 ART is 02:30 UTC the next day; 00:30 ART is 03:30 UTC. A
		// UTC (or server-zone) day would call these the same day.
		{"across midnight in Argentina", time.Date(2026, 10, 20, 23, 30, 0, 0, art), time.Date(2026, 10, 21, 0, 30, 0, 0, art), true},
		// Both are on the 21st in UTC for 21:00 ART? No: 21:00 ART is
		// 00:00 UTC the next day, 23:30 ART is 02:30 UTC. One ART day.
		{"same Argentina day, across UTC midnight", time.Date(2026, 10, 20, 21, 0, 0, 0, art), time.Date(2026, 10, 20, 23, 30, 0, 0, art), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newPhotoEnv(t)
			c := e.signUp(t, "ana@example.com", "ana", true)

			e.clock.Set(tt.first.UTC())
			wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)

			e.clock.Set(tt.next.UTC())
			want := http.StatusUnprocessableEntity
			if tt.wantAllowed {
				want = http.StatusSeeOther
			}
			wantStatus(t, e.upload(t, c, testJPEG(t, 44, 22)), want)
		})
	}
}

func TestPhotoUpload_NotAllowlisted(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	if home := e.home(t, c); strings.Contains(home, boxTitle) || strings.Contains(home, `name="photo"`) {
		t.Error("dashboard shows the photo box to an account that is not allowlisted")
	}
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusForbidden)
	if got := e.files(t); len(got) != 0 {
		t.Errorf("store has %v, want nothing", got)
	}
}

func TestPhotoUpload_Errors(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	var gif bytes.Buffer
	// A minimal GIF89a header with a 1x1 screen: decodes as GIF in no way we
	// accept, which is all this needs.
	gif.WriteString("GIF89a\x01\x00\x01\x00\x00\x00\x00;")

	noPart, ctype := multipartBody(t, "photo", "", nil)
	wrongName, ctype2 := multipartBody(t, "avatar", "a.jpg", testJPEG(t, 20, 20))
	emptyPart, ctype3 := multipartBody(t, "photo", "a.jpg", []byte{})

	tests := []struct {
		name  string
		do    func() reply
		want  int
		wants string
	}{
		{"no file part", func() reply { return e.post(t, c, "/photo", ctype, noPart) }, 422, msgNone},
		{"part with another name", func() reply { return e.post(t, c, "/photo", ctype2, wrongName) }, 422, msgNone},
		{"empty part", func() reply { return e.post(t, c, "/photo", ctype3, emptyPart) }, 422, msgNone},
		{"not multipart", func() reply {
			return e.post(t, c, "/photo", "application/x-www-form-urlencoded", strings.NewReader("photo=x"))
		}, 422, msgNone},
		{"gif", func() reply { return e.upload(t, c, gif.Bytes()) }, 422, msgUnreadble},
		{"not an image", func() reply { return e.upload(t, c, []byte("hello, not a photo")) }, 422, msgUnreadble},
		{"decompression bomb", func() reply { return e.upload(t, c, bombPNG(30000, 30000)) }, 422, msgTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantBodyHas(t, tt.do(), tt.want, tt.wants)
			if got := e.files(t); len(got) != 0 {
				t.Errorf("store has %v, want no new file", got)
			}
			// A failed upload must not use up the day.
			if home := e.home(t, c); strings.Contains(home, msgToday) {
				t.Error("a rejected upload counted as today's upload")
			}
		})
	}
}

func TestPhotoUpload_AcceptsPNGWithTransparency(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	wantStatus(t, e.upload(t, c, testPNG(t, 30, 30, color.NRGBA{R: 255, A: 0})), http.StatusSeeOther)
	key := e.currentKey(t, c)
	body := e.status(t, anaHost, "/photo/"+key+".jpg").Body
	img, err := jpeg.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	r, g, b, _ := img.At(15, 15).RGBA()
	if r>>8 < 200 || g>>8 < 200 || b>>8 < 200 {
		t.Errorf("transparent pixel became %d,%d,%d, want near paper white, not black", r>>8, g>>8, b>>8)
	}
}

func TestPhotoUpload_BodyLimit(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	t.Run("declared length over the cap is a 413", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/photo", strings.NewReader("tiny"))
		req.Host = appHost
		req.ContentLength = 11<<20 + 1
		req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
		req.AddCookie(c)
		wantStatus(t, e.do(req), http.StatusRequestEntityTooLarge)
		if got := e.files(t); len(got) != 0 {
			t.Errorf("store has %v, want nothing", got)
		}
	})

	t.Run("an understated length is cut off by the reader", func(t *testing.T) {
		// A body bigger than the cap with no usable Content-Length.
		// The first part is a 12 MiB field the handler must skip over to reach
		// the photo, so the reader hits the cap before it sees the photo.
		big := io.MultiReader(strings.NewReader("--x\r\nContent-Disposition: form-data; name=\"filler\"\r\n\r\n"),
			io.LimitReader(zeros{}, 12<<20),
			strings.NewReader("\r\n--x--\r\n"))
		req := httptest.NewRequest(http.MethodPost, "/photo", big)
		req.Host = appHost
		req.ContentLength = -1
		req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
		req.AddCookie(c)
		wantStatus(t, e.do(req), http.StatusRequestEntityTooLarge)
		if got := e.files(t); len(got) != 0 {
			t.Errorf("store has %v, want nothing", got)
		}
	})

	t.Run("other routes keep the 4 KiB limit", func(t *testing.T) {
		for _, path := range []string{"/mood", "/name", "/photo/remove"} {
			resp := e.post(t, c, path, "application/x-www-form-urlencoded",
				strings.NewReader("note="+strings.Repeat("a", 5<<10)))
			wantStatus(t, resp, http.StatusRequestEntityTooLarge)
		}
	})

	t.Run("the exemption is POST /photo only", func(t *testing.T) {
		// Same path, other methods and near-miss paths stay limited.
		for _, tc := range []struct{ method, path string }{
			{http.MethodPut, "/photo"},
			{http.MethodPost, "/photo/"},
			{http.MethodPost, "/photo/x"},
			{http.MethodPost, "/Photo"},
		} {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("x"))
			req.Host = appHost
			req.ContentLength = 5 << 10
			req.AddCookie(c)
			wantStatus(t, e.do(req), http.StatusRequestEntityTooLarge)
		}
	})
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestPhotoUpload_Busy(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)
	e.srv.SetPhotoSlotWait(50 * time.Millisecond)

	release := e.srv.HoldPhotoSlot()
	wantBodyHas(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusServiceUnavailable, msgBusy)
	if got := e.files(t); len(got) != 0 {
		t.Errorf("store has %v, want nothing", got)
	}
	release()

	// Once the slot is free the same upload goes through.
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)
}

func TestPhotoUpload_Guards(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	t.Run("signed out is sent to login", func(t *testing.T) {
		resp := e.upload(t, nil, testJPEG(t, 40, 20))
		wantStatus(t, resp, http.StatusSeeOther)
		if loc := resp.Header.Get("Location"); loc != "/login" {
			t.Errorf("Location = %q, want /login", loc)
		}
	})

	t.Run("cross-origin is refused", func(t *testing.T) {
		wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20), "Origin", "https://evil.example"), http.StatusForbidden)
		wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20), "Sec-Fetch-Site", "cross-site"), http.StatusForbidden)
		if got := e.files(t); len(got) != 0 {
			t.Errorf("store has %v, want nothing", got)
		}
	})

	t.Run("a same-origin browser upload is allowed", func(t *testing.T) {
		wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20),
			"Origin", "https://retratar.com.ar", "Sec-Fetch-Site", "same-origin"), http.StatusSeeOther)
	})

	t.Run("an account without a handle is sent home", func(t *testing.T) {
		e2 := newPhotoEnv(t)
		c2 := completeLogin(t, e2.h, appHost, e2.send, e2.srv.Config.BaseURL(), "bob@example.com")
		if _, err := e2.pool.Exec(t.Context(), `UPDATE users SET can_upload_photos = true`); err != nil {
			t.Fatal(err)
		}
		resp := e2.upload(t, c2, testJPEG(t, 40, 20))
		wantStatus(t, resp, http.StatusSeeOther)
		if got := e2.files(t); len(got) != 0 {
			t.Errorf("store has %v, want nothing", got)
		}
	})
}

func TestPhotoRemove(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)
	key := e.currentKey(t, c)

	wantStatus(t, e.post(t, c, "/photo/remove", "application/x-www-form-urlencoded", strings.NewReader("")), http.StatusSeeOther)

	if _, err := os.Stat(filepath.Join(e.dir, key+".jpg")); !os.IsNotExist(err) {
		t.Errorf("file still on disk (stat error = %v)", err)
	}
	if resp := e.status(t, anaHost, "/photo/"+key+".jpg"); resp.Code != http.StatusNotFound {
		t.Errorf("page-host URL status = %d, want 404", resp.Code)
	}

	// Uploaded today, so the form stays hidden even with no photo.
	home := e.home(t, c)
	if photoSrc.MatchString(home) || strings.Contains(home, `action="/photo/remove"`) {
		t.Error("dashboard still shows a photo or the remove button")
	}
	if strings.Contains(home, `action="/photo" enctype`) {
		t.Error("remove-and-upload must not bypass the daily limit, but the form is back")
	}
	wantBodyHas(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusUnprocessableEntity, msgToday)

	// The next day the form returns.
	e.clock.Set(e.clock.Now().Add(24 * time.Hour))
	if home := e.home(t, c); !strings.Contains(home, `action="/photo" enctype`) {
		t.Error("next day: dashboard does not offer the upload form")
	}

	// Removing with no photo is a no-op.
	wantStatus(t, e.post(t, c, "/photo/remove", "application/x-www-form-urlencoded", strings.NewReader("")), http.StatusSeeOther)
}

func TestPhotoRemove_WorksAfterLosingUploadRights(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)
	key := e.currentKey(t, c)

	if _, err := e.pool.Exec(t.Context(), `UPDATE users SET can_upload_photos = false`); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.post(t, c, "/photo/remove", "application/x-www-form-urlencoded", strings.NewReader("")), http.StatusSeeOther)
	if _, err := os.Stat(filepath.Join(e.dir, key+".jpg")); !os.IsNotExist(err) {
		t.Errorf("file still on disk (stat error = %v)", err)
	}
}

func TestPhotoRemove_Guards(t *testing.T) {
	e := newPhotoEnv(t)
	resp := e.post(t, nil, "/photo/remove", "application/x-www-form-urlencoded", strings.NewReader(""))
	wantStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func TestPhotoServe_WrongHost(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)
	e.signUp(t, "bob@example.com", "bob", true)
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)
	key := e.currentKey(t, c)

	if resp := e.status(t, bobHost, "/photo/"+key+".jpg"); resp.Code != http.StatusNotFound {
		t.Errorf("A's key on B's host: status = %d, want 404", resp.Code)
	}
	if resp := e.status(t, anaHost, "/photo/"+key+".jpg"); resp.Code != http.StatusOK {
		t.Errorf("A's key on A's host: status = %d, want 200", resp.Code)
	}
	// Case in the host does not matter.
	if resp := e.status(t, "ANA.retrat.ar", "/photo/"+key+".jpg"); resp.Code != http.StatusOK {
		t.Errorf("A's key on ANA.retrat.ar: status = %d, want 200", resp.Code)
	}
}

func TestPhotoServe_AppHostOwnerOnly(t *testing.T) {
	e := newPhotoEnv(t)
	ana := e.signUp(t, "ana@example.com", "ana", true)
	bob := e.signUp(t, "bob@example.com", "bob", true)
	wantStatus(t, e.upload(t, ana, testJPEG(t, 40, 20)), http.StatusSeeOther)
	key := e.currentKey(t, ana)
	path := "/photo/" + key + ".jpg"

	resp := e.status(t, appHost, path, ana)
	if resp.Code != http.StatusOK {
		t.Fatalf("owner: status = %d, want 200", resp.Code)
	}
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("owner: Cache-Control = %q, want no-store", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("owner: Content-Type = %q, want image/jpeg", got)
	}
	if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got != "same-site" {
		t.Errorf("owner: CORP = %q, want same-site (must not be relaxed)", got)
	}
	if _, err := jpeg.Decode(bytes.NewReader(resp.Body)); err != nil {
		t.Errorf("owner: body does not decode: %v", err)
	}

	if resp := e.status(t, appHost, path, bob); resp.Code != http.StatusNotFound {
		t.Errorf("other user: status = %d, want 404", resp.Code)
	}
	if resp := e.status(t, appHost, path); resp.Code != http.StatusNotFound {
		t.Errorf("no session: status = %d, want 404", resp.Code)
	}

	// After a replacement the old key is no longer the owner's current photo.
	e.clock.Set(e.clock.Now().Add(24 * time.Hour))
	wantStatus(t, e.upload(t, ana, testJPEG(t, 40, 20)), http.StatusSeeOther)
	if resp := e.status(t, appHost, path, ana); resp.Code != http.StatusNotFound {
		t.Errorf("owner, replaced key: status = %d, want 404", resp.Code)
	}
}

func TestPhotoServe_BadFile(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)
	key := e.currentKey(t, c)

	for _, path := range []string{
		"/photo/..%2Fx.jpg",
		"/photo/%2e%2e%2fx.jpg",
		"/photo/" + key + ".png",
		"/photo/" + key,
		"/photo/" + key + ".jpg.jpg",
		"/photo/" + strings.ToUpper(key) + ".jpg",
		"/photo/.jpg",
		"/photo/",
	} {
		for _, host := range []string{anaHost, appHost} {
			resp := e.status(t, host, path, c)
			if resp.Code != http.StatusNotFound {
				t.Errorf("GET %s%s status = %d, want 404", host, path, resp.Code)
			}
		}
	}

	// A literal ".." is cleaned by ServeMux into a redirect before any handler
	// runs; what matters is that it never serves a file.
	resp := e.status(t, anaHost, "/photo/../x.jpg")
	if resp.Code == http.StatusOK {
		t.Errorf("GET /photo/../x.jpg status = 200, want a redirect or 404")
	}
}

func TestPhotoServe_MissingFileIs404(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)
	key := e.currentKey(t, c)
	if err := os.Remove(filepath.Join(e.dir, key+".jpg")); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{anaHost, appHost} {
		if resp := e.status(t, host, "/photo/"+key+".jpg", c); resp.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", host, resp.Code)
		}
	}
}

func TestPhotoServe_PagesSurfaceStillNeverSetsCookies(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)
	key := e.currentKey(t, c)
	resp := e.status(t, anaHost, "/photo/"+key+".jpg")
	if len(resp.Cookies) != 0 {
		t.Errorf("page-host photo set cookies: %v", resp.Cookies)
	}
}

func TestPhotoUpload_PanicReleasesSlot(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	e.srv.SetPhotoProcess(func(io.Reader) (photo.Result, error) { panic("decoder blew up") })
	// The Recover middleware turns the panic into a 500 for this request.
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusInternalServerError)

	// The slot must be free again: a working processor now succeeds, not 503.
	e.srv.SetPhotoProcess(nil)
	e.srv.SetPhotoSlotWait(50 * time.Millisecond)
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)
}
