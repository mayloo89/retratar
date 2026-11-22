package web_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestIcons_Served(t *testing.T) {
	e := newPhotoEnv(t)
	for _, host := range []string{appHost, "ana.retrat.ar"} {
		for path, ctype := range map[string]string{
			"/favicon.svg":          "image/svg+xml",
			"/apple-touch-icon.png": "image/png",
			"/favicon.ico":          "image/png",
		} {
			r := e.status(t, host, path)
			wantStatus(t, r, http.StatusOK)
			if got := r.Header.Get("Content-Type"); got != ctype {
				t.Errorf("%s%s Content-Type = %q, want %q", host, path, got, ctype)
			}
			// The app surface's PrivateCache marks everything no-store, icons
			// included; the pages surface carries the handler's own header.
			if host != appHost {
				if got := r.Header.Get("Cache-Control"); got != "public, max-age=86400" {
					t.Errorf("%s%s Cache-Control = %q, want public, max-age=86400", host, path, got)
				}
			}
		}
	}

	r := e.status(t, appHost, "/apple-touch-icon.png")
	img, err := png.Decode(bytes.NewReader(r.Body))
	if err != nil {
		t.Fatalf("decode apple-touch-icon.png: %v", err)
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 180, 180) {
		t.Errorf("apple-touch-icon.png bounds = %v, want 180×180", got)
	}
	for _, p := range []struct {
		x, y int
		want color.RGBA
	}{
		{90, 60, color.RGBA{0xf2, 0xc9, 0x4c, 0xff}}, // the photo
		{5, 5, color.RGBA{0x57, 0x38, 0x1f, 0xff}},   // the tile
	} {
		if got := color.RGBAModel.Convert(img.At(p.x, p.y)); got != p.want {
			t.Errorf("pixel (%d, %d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}
}

func TestErrorPage_UnknownPath(t *testing.T) {
	e := newPhotoEnv(t)
	e.signUp(t, "ana@example.com", "ana", false)

	for _, tc := range []struct {
		host, base string
		links      []string
	}{
		{appHost, "", []string{`href="/random"`, `href="/">Volver al inicio`}},
		{"ana.retrat.ar", "https://retratar.com.ar", []string{
			`href="https://retratar.com.ar/random"`,
			`href="https://retratar.com.ar/">Hacé tu página en retratar`,
			`href="https://retratar.com.ar/privacy"`,
		}},
	} {
		r := e.status(t, tc.host, "/nope")
		wantErrorPage(t, r, http.StatusNotFound, "Esta página no existe")
		for _, want := range tc.links {
			if !strings.Contains(string(r.Body), want) {
				t.Errorf("%s/nope body = %q, want it to contain %q", tc.host, r.Body, want)
			}
		}
	}
}

func TestErrorPage_UnclaimedHandle(t *testing.T) {
	e := newPhotoEnv(t)

	r := e.status(t, "nadie.retrat.ar", "/")
	wantErrorPage(t, r, http.StatusNotFound, "Esta página no existe")
	// The same page for unclaimed, reserved and deleted handles: it neither
	// echoes the handle nor promises it is free.
	for _, banned := range []string{"nadie", "libre"} {
		if strings.Contains(string(r.Body), banned) {
			t.Errorf("body = %q, want no %q", r.Body, banned)
		}
	}
}

func TestErrorPage_NoRequestDataEchoed(t *testing.T) {
	e := newPhotoEnv(t)
	e.signUp(t, "ana@example.com", "ana", false)

	for _, host := range []string{appHost, "ana.retrat.ar"} {
		r := e.status(t, host, "/%3Cscript%3Ex?q=%3Cb%3E")
		wantErrorPage(t, r, http.StatusNotFound, "Esta página no existe")
		for _, banned := range []string{"script", "<b>", "q="} {
			if strings.Contains(string(r.Body), banned) {
				t.Errorf("%s body = %q, want no %q", host, r.Body, banned)
			}
		}
	}
}

func TestErrorPage_UnknownHostStaysPlain(t *testing.T) {
	e := newPhotoEnv(t)

	r := e.status(t, "evil.example", "/")
	if r.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", r.Code)
	}
	if got := string(r.Body); got != "404 page not found\n" {
		t.Errorf("body = %q, want Go's plain 404", got)
	}
	if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", got)
	}
}

func TestAppHome_Notice(t *testing.T) {
	e := newPhotoEnv(t)
	s := seedAnaAndBob(t, e)

	for key, want := range map[string]struct{ box, text string }{
		"mood":          {"estado", "Listo. Tu página ya muestra cómo estás."},
		"name":          {"nombre", "Listo. Guardamos tu nombre."},
		"photo":         {"foto", "Listo. Tu foto nueva ya está en tu página."},
		"photo-removed": {"foto", "Listo. Sacamos la foto de tu página."},
		"style":         {"estilo", "Listo. Tu estilo ya está publicado."},
		// Ana's book has bob's entry, so this is the list, not the empty state.
		"entry-hidden": {"libro", "Listo. Esa firma ya no se ve en tu página."},
		"entry-shown":  {"libro", "Listo. Esa firma se ve de nuevo."},
	} {
		r := e.status(t, appHost, "/?ok="+key, s.ana)
		wantStatus(t, r, http.StatusOK)
		body := string(r.Body)
		if n := strings.Count(body, `class="ok"`); n != 1 {
			t.Errorf("?ok=%s: %d notices, want 1", key, n)
		}
		section := sectionOf(body, want.box)
		if !strings.Contains(section, `<p class="ok" role="status">`+want.text+`</p>`) {
			t.Errorf("?ok=%s: section #%s = %q, want the notice %q", key, want.box, section, want.text)
		}
	}

	for _, ok := range []string{"nope", url.QueryEscape("<script>"), ""} {
		r := e.status(t, appHost, "/?ok="+ok, s.ana)
		wantStatus(t, r, http.StatusOK)
		if strings.Contains(string(r.Body), `class="ok"`) {
			t.Errorf("?ok=%s shows a notice, want none", ok)
		}
	}
}

func TestDashboard_SuccessRedirects(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)

	for _, tc := range []struct {
		name string
		do   func() reply
		want string
	}{
		{"mood", func() reply { return e.form(t, c, "/mood", url.Values{"mood_key": {"feliz"}}) }, "/?ok=mood#estado"},
		{"photo upload", func() reply { return e.upload(t, c, testJPEG(t, 40, 20)) }, "/?ok=photo#foto"},
		{"photo remove", func() reply { return e.form(t, c, "/photo/remove", url.Values{}) }, "/?ok=photo-removed#foto"},
	} {
		r := tc.do()
		wantStatus(t, r, http.StatusSeeOther)
		if got := r.Header.Get("Location"); got != tc.want {
			t.Errorf("%s Location = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// wantErrorPage checks r is the styled error page with title, never cached.
func wantErrorPage(t *testing.T, r reply, status int, title string) {
	t.Helper()
	wantStatus(t, r, status)
	if got := r.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
	if got := r.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if !strings.Contains(string(r.Body), "<h1>"+title+"</h1>") {
		t.Errorf("body = %q, want the title %q", r.Body, title)
	}
}

// sectionOf returns the dashboard's <section id="id">, up to its </section>.
func sectionOf(body, id string) string {
	if _, rest, ok := strings.Cut(body, `<section class="box" id="`+id+`">`); ok {
		section, _, _ := strings.Cut(rest, "</section>")
		return section
	}
	// A block inside a box, like #nombre in Tu cuenta, ends where the box's
	// own back link starts.
	_, rest, ok := strings.Cut(body, `<div id="`+id+`">`)
	if !ok {
		return ""
	}
	section, _, _ := strings.Cut(rest, `<p class="back">`)
	return section
}
