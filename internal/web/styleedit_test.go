package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mayloo89/retratar/internal/style"
)

// styleForm is the "Tu estilo" form an untouched dashboard would submit for an
// account whose mood is moodKey: every colour at its default under the mood,
// both fonts at their defaults, and the "cómo estoy" box ticked (keeps the mood
// colours). set overrides fields; a nil value removes one.
func styleForm(moodKey string, set map[string]*string) url.Values {
	f := url.Values{}
	for _, slot := range style.Slots() {
		f.Set("c_"+slot.Key, style.Default(slot.Key, moodKey))
	}
	f.Set("f_head", style.DefaultHeadFont)
	f.Set("f_body", style.DefaultBodyFont)
	f.Set("own_mood_box", "no")
	for k, v := range set {
		if v == nil {
			f.Del(k)
		} else {
			f.Set(k, *v)
		}
	}
	return f
}

func with(kv ...string) map[string]*string {
	m := map[string]*string{}
	for i := 0; i+1 < len(kv); i += 2 {
		v := kv[i+1]
		m[kv[i]] = &v
	}
	return m
}

func without(keys ...string) map[string]*string {
	m := map[string]*string{}
	for _, k := range keys {
		m[k] = nil
	}
	return m
}

func (e *photoEnv) publish(t *testing.T, c *http.Cookie, moodKey string, set map[string]*string) {
	t.Helper()
	f := styleForm(moodKey, set)
	f.Set("action", "publish")
	r := e.form(t, c, "/style", f)
	wantStatus(t, r, http.StatusSeeOther)
	if loc := r.Header.Get("Location"); loc != "/" {
		t.Fatalf("publish Location = %q, want /", loc)
	}
}

func (e *photoEnv) styleRows(t *testing.T) int {
	t.Helper()
	return e.count(t, `SELECT count(*) FROM page_styles`)
}

func getReply(t *testing.T, e *photoEnv, host, path string) reply {
	t.Helper()
	return e.status(t, host, path)
}

func TestStylePublish_UntouchedFormSavesNothing(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	e.publish(t, c, "none", nil)
	if n := e.styleRows(t); n != 0 {
		t.Errorf("page_styles rows = %d, want 0: an untouched form must not turn defaults into Personalizado", n)
	}

	// Same under a mood: the form submits that mood's defaults.
	wantStatus(t, e.form(t, c, "/mood", url.Values{"mood_key": {"enamorado"}}), http.StatusSeeOther)
	e.publish(t, c, "enamorado", nil)
	if n := e.styleRows(t); n != 0 {
		t.Errorf("page_styles rows under a mood = %d, want 0", n)
	}
}

func TestStylePublish_OneChangedColourStoresOneSlot(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	e.publish(t, c, "none", with("c_link", "#0000EE"))

	st, err := e.srv.Users.GetStyle(t.Context(), mustID(t, e, "ana"))
	if err != nil {
		t.Fatalf("GetStyle() error = %v", err)
	}
	if len(st.Colors) != 1 || st.Colors["link"] != "#0000ee" {
		t.Errorf("stored colours = %v, want only link=#0000ee", st.Colors)
	}
	if st.HeadFont != "" || st.BodyFont != "" || st.OwnMoodBox {
		t.Errorf("stored style = %+v, want nothing else set", st)
	}
	if !st.UpdatedAt.Equal(e.clock.Now()) {
		t.Errorf("UpdatedAt = %v, want the app clock %v", st.UpdatedAt, e.clock.Now())
	}
}

func TestStylePublish_FontsAndMoodBox(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	// A font equal to the default is Por defecto; a different one is stored.
	e.publish(t, c, "none", with("f_head", "trebuchet", "f_body", "comic"))
	st, _ := e.srv.Users.GetStyle(t.Context(), mustID(t, e, "ana"))
	if st.HeadFont != "" || st.BodyFont != "comic" {
		t.Errorf("fonts = %q/%q, want default/comic", st.HeadFont, st.BodyFont)
	}
	if st.OwnMoodBox {
		t.Error("OwnMoodBox = true after a form with the box ticked")
	}

	// Unticked: the field is absent, which means the box takes the owner's colours.
	e.publish(t, c, "none", without("own_mood_box"))
	st, _ = e.srv.Users.GetStyle(t.Context(), mustID(t, e, "ana"))
	if !st.OwnMoodBox {
		t.Error("OwnMoodBox = false after unchecking the checkbox, want true")
	}
	if n := e.count(t, `SELECT count(*) FROM page_styles WHERE own_mood_box`); n != 1 {
		t.Errorf("rows with own_mood_box = %d, want 1", n)
	}

	// Back to all default deletes the row.
	e.publish(t, c, "none", nil)
	if n := e.styleRows(t); n != 0 {
		t.Errorf("page_styles rows = %d, want 0 after publishing all defaults", n)
	}
}

func mustID(t *testing.T, e *photoEnv, handle string) uuid.UUID {
	t.Helper()
	u, err := e.srv.Users.GetByHandle(t.Context(), handle)
	if err != nil {
		t.Fatalf("GetByHandle(%q) error = %v", handle, err)
	}
	return u.ID
}

func TestStyleReset_ClearsOnlyThatSlotAndStoresNothing(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)
	e.publish(t, c, "none", with("c_bg", "#ff00cc", "c_link", "#0000ee"))

	// The dashboard form as the browser sends it, with Restablecer on Fondo.
	f := styleForm("none", with("c_bg", "#ff00cc", "c_link", "#0000ee"))
	f.Set("reset", "bg")
	r := e.form(t, c, "/style", f)
	wantStatus(t, r, http.StatusOK)
	body := string(r.Body)

	wantContains(t, body,
		`id="st_bg">Por defecto<`,
		`name="c_bg" value="`+style.Default("bg", "none")+`"`,
		`id="st_link">Personalizado<`,
		`name="c_link" value="#0000ee"`,
		`Tenés cambios sin publicar.`,
	)
	if strings.Contains(body, `name="reset" value="bg"`) {
		t.Error("a Por defecto row shows Restablecer")
	}
	wantContains(t, body, `name="reset" value="link"`)

	st, _ := e.srv.Users.GetStyle(t.Context(), mustID(t, e, "ana"))
	if st.Colors["bg"] != "#ff00cc" || st.Colors["link"] != "#0000ee" {
		t.Errorf("stored colours = %v, want both still stored: Restablecer must not publish", st.Colors)
	}

	// A font row's Restablecer.
	e.publish(t, c, "none", with("f_head", "impact"))
	f = styleForm("none", with("f_head", "impact"))
	f.Set("reset", "head")
	r = e.form(t, c, "/style", f)
	wantStatus(t, r, http.StatusOK)
	wantContains(t, string(r.Body), `id="st_f_head">Por defecto<`, `<option value="trebuchet" selected>`)
}

func TestStyleEdit_ReturnsTheDraft(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	f := styleForm("none", with("c_name", "#123456", "f_body", "courier"))
	f.Set("action", "edit")
	r := e.form(t, c, "/style", f)
	wantStatus(t, r, http.StatusOK)
	wantContains(t, string(r.Body),
		`name="c_name" value="#123456"`,
		`id="st_name">Personalizado<`,
		`<option value="courier" selected>`,
		`Tenés cambios sin publicar.`,
	)
	if n := e.styleRows(t); n != 0 {
		t.Errorf("page_styles rows = %d, want 0: edit stores nothing", n)
	}

	// A draft equal to what is stored has nothing unpublished.
	e.publish(t, c, "none", with("c_name", "#123456"))
	f = styleForm("none", with("c_name", "#123456"))
	f.Set("action", "edit")
	r = e.form(t, c, "/style", f)
	wantStatus(t, r, http.StatusOK)
	if strings.Contains(string(r.Body), "Tenés cambios sin publicar.") {
		t.Error("the notice shows although the draft equals the stored style")
	}
}

func TestStylePreview_RendersThePageWithTheDraft(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)
	wantStatus(t, e.form(t, c, "/mood", url.Values{"mood_key": {"feliz"}, "note": {"domingo"}}), http.StatusSeeOther)

	f := styleForm("feliz", with("c_bg", "#ff00cc", "f_head", "impact"))
	f.Del("own_mood_box")
	f.Set("action", "preview")
	r := e.form(t, c, "/style", f)
	wantStatus(t, r, http.StatusOK)
	body := string(r.Body)

	wantContains(t, body,
		`<meta name="robots" content="noindex">`,
		`<form class="pv-bar" method="post" action="/style">`,
		`Así se va a ver tu página.`,
		`<button class="pv-publish" name="action" value="publish">Publicar</button>`,
		`<button class="pv-edit" name="action" value="edit">Seguir cambiando</button>`,
		`<body class="mood-feliz own-mood-box">`,
		`domingo`,
		`<h1 class="blk-name">ana</h1>`,
	)

	// Stylesheets, in order: theme, the draft, the bar's.
	theme := strings.Index(body, `href="/theme.css"`)
	draft := strings.Index(body, `href="/style/preview.css?c_bg=%23ff00cc&amp;f_head=impact&amp;own=1"`)
	bar := strings.Index(body, `href="/preview.css"`)
	if theme < 0 || draft < theme || bar < draft {
		t.Errorf("stylesheet order theme=%d draft=%d bar=%d, want theme < draft < preview bar", theme, draft, bar)
	}
	if strings.Contains(body, "/style.css") {
		t.Error("the preview links the published stylesheet")
	}
	if strings.Contains(body, ` style="`) || strings.Contains(body, "<style") {
		t.Error("the preview has inline styles, which the CSP blocks")
	}
	if bar, main := strings.Index(body, `class="pv-bar"`), strings.Index(body, "<main"); bar > main {
		t.Error("the preview bar is not before <main>")
	}

	// The hidden inputs are the whole draft, in the form's own field names.
	hidden := regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`).FindAllStringSubmatch(body, -1)
	got := map[string]string{}
	for _, m := range hidden {
		got[m[1]] = m[2]
	}
	want := map[string]string{"f_head": "impact", "f_body": style.DefaultBodyFont}
	for _, slot := range style.Slots() {
		want["c_"+slot.Key] = style.Default(slot.Key, "feliz")
	}
	want["c_bg"] = "#ff00cc"
	if len(got) != len(want) {
		t.Errorf("hidden inputs = %v, want %v (no own_mood_box: the box takes the owner's colours)", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("hidden %s = %q, want %q", k, got[k], v)
		}
	}
	if n := e.styleRows(t); n != 0 {
		t.Errorf("page_styles rows = %d, want 0: preview stores nothing", n)
	}

	// The bar's own buttons post that draft: publishing it stores it.
	pub := url.Values{"action": {"publish"}}
	for k, v := range got {
		pub.Set(k, v)
	}
	wantStatus(t, e.form(t, c, "/style", pub), http.StatusSeeOther)
	st, _ := e.srv.Users.GetStyle(t.Context(), mustID(t, e, "ana"))
	if len(st.Colors) != 1 || st.Colors["bg"] != "#ff00cc" || st.HeadFont != "impact" || !st.OwnMoodBox {
		t.Errorf("stored after publishing from the bar = %+v, want bg, impact and OwnMoodBox", st)
	}
}

func TestStylePreview_ShowsThePhotoFromTheAppHost(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", true)
	wantStatus(t, e.upload(t, c, testJPEG(t, 40, 20)), http.StatusSeeOther)

	f := styleForm("none", nil)
	f.Set("action", "preview")
	r := e.form(t, c, "/style", f)
	wantStatus(t, r, http.StatusOK)
	m := regexp.MustCompile(`<img src="(/photo/[a-z2-7]{26}\.jpg)"`).FindStringSubmatch(string(r.Body))
	if m == nil {
		t.Fatalf("preview has no same-origin photo; body: %.600s", r.Body)
	}
	img := e.status(t, appHost, m[1], c)
	wantStatus(t, img, http.StatusOK)
	if ct := img.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("photo Content-Type = %q, want image/jpeg", ct)
	}
}

func TestStyleSubmit_InvalidInputIs422AndKeepsTheStoredStyle(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)
	e.publish(t, c, "none", with("c_bg", "#ff00cc"))

	for name, f := range map[string]url.Values{
		"bad hex":       styleForm("none", with("c_bg", "red")),
		"short hex":     styleForm("none", with("c_link", "#abc")),
		"injection":     styleForm("none", with("c_text", "#000000;}body{x:y")),
		"unknown font":  styleForm("none", with("f_head", "wingdings")),
		"missing slot":  styleForm("none", without("c_name")),
		"missing font":  styleForm("none", without("f_body")),
		"repeated slot": func() url.Values { v := styleForm("none", nil); v.Add("c_bg", "#000000"); return v }(),
	} {
		for _, action := range []string{"publish", "preview", "edit"} {
			t.Run(name+"/"+action, func(t *testing.T) {
				f.Set("action", action)
				r := e.form(t, c, "/style", f)
				wantStatus(t, r, http.StatusUnprocessableEntity)
				wantContains(t, string(r.Body), `<p role="alert">No pudimos guardar ese estilo. Probá de nuevo.</p>`)
				// The box keeps showing what is stored.
				wantContains(t, string(r.Body), `name="c_bg" value="#ff00cc"`)
			})
		}
	}
	st, _ := e.srv.Users.GetStyle(t.Context(), mustID(t, e, "ana"))
	if len(st.Colors) != 1 || st.Colors["bg"] != "#ff00cc" {
		t.Errorf("stored style = %+v, want it untouched by the rejected forms", st)
	}
}

func TestStyleSubmit_BadActionIs400(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	for name, extra := range map[string]url.Values{
		"unknown action":   {"action": {"delete"}},
		"no action":        {},
		"empty action":     {"action": {""}},
		"unknown reset":    {"reset": {"nope"}},
		"reset wrong case": {"reset": {"BG"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := styleForm("none", nil)
			for k, v := range extra {
				f[k] = v
			}
			wantStatus(t, e.form(t, c, "/style", f), http.StatusBadRequest)
		})
	}
	if n := e.styleRows(t); n != 0 {
		t.Errorf("page_styles rows = %d, want 0", n)
	}
}

func TestStyleSubmit_Guards(t *testing.T) {
	e := newPhotoEnv(t)
	f := styleForm("none", with("c_bg", "#ff00cc"))
	f.Set("action", "publish")

	t.Run("signed out", func(t *testing.T) {
		r := e.form(t, nil, "/style", f)
		wantStatus(t, r, http.StatusSeeOther)
		if loc := r.Header.Get("Location"); loc != "/login" {
			t.Errorf("Location = %q, want /login", loc)
		}
	})
	t.Run("no handle", func(t *testing.T) {
		c := completeLogin(t, e.h, appHost, e.send, e.srv.Config.BaseURL(), "nohandle@example.com")
		r := e.form(t, c, "/style", f)
		wantStatus(t, r, http.StatusSeeOther)
		if loc := r.Header.Get("Location"); loc != "/" {
			t.Errorf("Location = %q, want /", loc)
		}
	})
	t.Run("cross-origin", func(t *testing.T) {
		c := e.signUp(t, "ana@example.com", "ana", false)
		for _, site := range []string{"cross-site", "same-site"} {
			r := e.form(t, c, "/style", f, "Sec-Fetch-Site", site)
			wantStatus(t, r, http.StatusForbidden)
		}
	})
	if n := e.styleRows(t); n != 0 {
		t.Errorf("page_styles rows = %d, want 0: no guarded request may store a style", n)
	}
}

func TestStyleBox_RendersEachRow(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)
	wantStatus(t, e.form(t, c, "/mood", url.Values{"mood_key": {"enamorado"}}), http.StatusSeeOther)

	home := e.home(t, c)
	wantContains(t, home, `<h2 class="bar">Tu estilo</h2>`,
		`Los colores y tipografías por defecto cambian según cómo estás.`,
		`<button name="action" value="preview">Ver cómo queda</button>`,
		`<button class="quiet" name="action" value="publish">Publicar</button>`,
		`<form method="post" action="/style">`)
	if strings.Contains(home, "Tenés cambios sin publicar.") {
		t.Error("the notice shows with nothing unpublished")
	}
	// Placement: after Tu nombre, before the guestbook.
	name, box, book := strings.Index(home, "Tu nombre</h2>"), strings.Index(home, "Tu estilo</h2>"), strings.Index(home, "Tu libro de visitas</h2>")
	if name >= box || box >= book {
		t.Errorf("box order name=%d style=%d guestbook=%d, want Tu nombre < Tu estilo < Tu libro de visitas", name, box, book)
	}

	// Every default slot shows the current mood's default and no Restablecer.
	for _, slot := range style.Slots() {
		wantContains(t, home,
			fmt.Sprintf(`<label for="c_%s">%s</label>`, slot.Key, slot.Label),
			fmt.Sprintf(`name="c_%s" value="%s"`, slot.Key, style.Default(slot.Key, "enamorado")),
			fmt.Sprintf(`id="st_%s">Por defecto<`, slot.Key),
		)
	}
	if strings.Contains(home, `name="reset"`) {
		t.Error("a Restablecer button shows with every slot at its default")
	}
	wantContains(t, home, `name="own_mood_box" value="no" checked>`)

	// The selects list the registry, in order, by label.
	for _, field := range []string{"f_head", "f_body"} {
		sel := regexp.MustCompile(`(?s)<select id="` + field + `".*?</select>`).FindString(home)
		var labels []string
		for _, m := range regexp.MustCompile(`<option value="[^"]+"(?: selected)?>([^<]+)</option>`).FindAllStringSubmatch(sel, -1) {
			labels = append(labels, m[1])
		}
		var want []string
		for _, f := range style.Fonts() {
			want = append(want, f.Label)
		}
		if strings.Join(labels, "|") != strings.Join(want, "|") {
			t.Errorf("%s options = %v, want the registry %v", field, labels, want)
		}
	}
	wantContains(t, home, `<option value="trebuchet" selected>`, `<option value="verdana" selected>`)

	// Customise three things and the rows follow.
	e.publish(t, c, "enamorado", with("c_bg", "#ff00cc", "c_link", "#0000ee", "f_head", "pixel"))
	wantStatus(t, e.form(t, c, "/mood", url.Values{"mood_key": {"triste"}}), http.StatusSeeOther)
	home = e.home(t, c)
	wantContains(t, home,
		`name="c_bg" value="#ff00cc"`, `id="st_bg">Personalizado<`, `name="reset" value="bg" aria-label="Restablecer Fondo">Restablecer</button>`,
		`name="c_link" value="#0000ee"`, `id="st_link">Personalizado<`, `name="reset" value="link"`,
		`<option value="pixel" selected>`, `id="st_f_head">Personalizado<`, `name="reset" value="head"`,
		// The rest follow the mood, and the mood is now triste.
		`name="c_sheet" value="`+style.Default("sheet", "triste")+`"`,
		`name="c_name" value="`+style.Default("name", "triste")+`"`,
		`id="st_f_body">Por defecto<`,
	)
	if n := strings.Count(home, `name="reset"`); n != 3 {
		t.Errorf("Restablecer buttons = %d, want 3 (one per customised row)", n)
	}

	// Unticking the box shows unchecked.
	e.publish(t, c, "triste", without("own_mood_box"))
	home = e.home(t, c)
	wantContains(t, home, `name="own_mood_box" value="no">`)
	if strings.Contains(home, `value="no" checked`) {
		t.Error("the checkbox is checked although the box takes the owner's colours")
	}
}

func TestStyleBox_AccessibleControls(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)
	home := e.home(t, c)

	for _, slot := range style.Slots() {
		wantContains(t, home, fmt.Sprintf(`id="c_%s" name="c_%s"`, slot.Key, slot.Key),
			fmt.Sprintf(`aria-describedby="st_%s"`, slot.Key))
	}
	for _, field := range []string{"f_head", "f_body"} {
		wantContains(t, home, `<label for="`+field+`">`, `aria-describedby="st_`+field+`"`)
	}
}

func wantContains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("body does not contain %q; body: %.1500s", w, body)
		}
	}
}

func TestStylePreviewCSS(t *testing.T) {
	e := newPhotoEnv(t)

	r := getReply(t, e, appHost, "/style/preview.css?c_bg=%23FF00CC&c_text=%2339ff14&f_head=impact&own=1")
	wantStatus(t, r, http.StatusOK)
	want := style.Style{
		Colors:   map[string]string{"bg": "#ff00cc", "text": "#39ff14"},
		HeadFont: "impact",
	}.CSS()
	if string(r.Body) != want {
		t.Errorf("body =\n%s\nwant the builder's output\n%s", r.Body, want)
	}
	if cc := r.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if ct := r.Header.Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	// An empty draft is valid and says nothing.
	r = getReply(t, e, appHost, "/style/preview.css")
	wantStatus(t, r, http.StatusOK)
	if len(r.Body) != 0 {
		t.Errorf("empty draft body = %q, want empty", r.Body)
	}

	for name, q := range map[string]string{
		"bad hex":        "c_bg=red",
		"hex injection":  "c_bg=%23ff0000%3B%7D",
		"unknown font":   "f_head=wingdings",
		"repeated slot":  "c_bg=%23000000&c_bg=%23111111",
		"empty colour":   "c_link=",
		"bad own":        "own=yes",
		"short hex":      "c_name=%23abc",
		"font injection": "f_body=x%3Bbackground%3Aurl(//evil)",
	} {
		r := getReply(t, e, appHost, "/style/preview.css?"+q)
		if r.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, r.Code)
		}
	}
}

func TestStylePreviewCSS_NotOnThePagesHost(t *testing.T) {
	e := newPhotoEnv(t)
	e.signUp(t, "ana@example.com", "ana", false)
	if r := getReply(t, e, anaHost, "/style/preview.css?c_bg=%23ff00cc"); r.Code != http.StatusNotFound {
		t.Errorf("pages host /style/preview.css status = %d, want 404", r.Code)
	}
	if r := getReply(t, e, anaHost, "/preview.css"); r.Code != http.StatusNotFound {
		t.Errorf("pages host /preview.css status = %d, want 404", r.Code)
	}
}

func TestAppHost_ServesThemeAndPreviewBarCSS(t *testing.T) {
	e := newPhotoEnv(t)

	r := getReply(t, e, appHost, "/theme.css")
	wantStatus(t, r, http.StatusOK)
	if ct := r.Header.Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("/theme.css Content-Type = %q", ct)
	}
	pageTheme := getReply(t, e, anaHost, "/theme.css")
	if string(r.Body) != string(pageTheme.Body) {
		t.Error("the app host's theme.css differs from the pages host's: it must be the same file")
	}

	r = getReply(t, e, appHost, "/preview.css")
	wantStatus(t, r, http.StatusOK)
	if ct := r.Header.Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("/preview.css Content-Type = %q", ct)
	}
	// Scoped: every selector starts with .pv-, so it cannot touch the page.
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(string(r.Body), "")
	selectors := regexp.MustCompile(`(?m)^([^{}\n][^{}]*)\{`).FindAllStringSubmatch(css, -1)
	if len(selectors) == 0 {
		t.Fatal("preview.css has no rules")
	}
	for _, m := range selectors {
		for _, sel := range strings.Split(m[1], ",") {
			if !strings.HasPrefix(strings.TrimSpace(sel), ".pv-") {
				t.Errorf("preview.css selector %q is not scoped under .pv-", strings.TrimSpace(sel))
			}
		}
	}
}

func TestStyleCSS_Served(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)
	e.publish(t, c, "none", with("c_bg", "#ff00cc", "f_body", "comic"))

	want := style.Style{Colors: map[string]string{"bg": "#ff00cc"}, BodyFont: "comic"}.CSS()
	r := getReply(t, e, anaHost, fmt.Sprintf("/style.css?v=%d", e.clock.Now().Unix()))
	wantStatus(t, r, http.StatusOK)
	if string(r.Body) != want {
		t.Errorf("body =\n%s\nwant\n%s", r.Body, want)
	}
	if ct := r.Header.Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := r.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want a year, immutable", cc)
	}

	// The value of v is ignored, but it must be there for the long cache.
	r = getReply(t, e, anaHost, "/style.css?v=whatever")
	wantStatus(t, r, http.StatusOK)
	if string(r.Body) != want {
		t.Error("a different v changed the body")
	}
	r = getReply(t, e, anaHost, "/style.css")
	wantStatus(t, r, http.StatusOK)
	if cc := r.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control without v = %q, want no-cache: nothing could bust it", cc)
	}
}

func TestStyleCSS_NotFound(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	if r := getReply(t, e, anaHost, "/style.css"); r.Code != http.StatusNotFound {
		t.Errorf("no style: status = %d, want 404", r.Code)
	}
	if r := getReply(t, e, "nobody.retrat.ar", "/style.css?v=1"); r.Code != http.StatusNotFound {
		t.Errorf("unknown handle: status = %d, want 404", r.Code)
	}
	// Only the mood box set: the row exists but there is no CSS to serve.
	e.publish(t, c, "none", without("own_mood_box"))
	if r := getReply(t, e, anaHost, "/style.css"); r.Code != http.StatusNotFound {
		t.Errorf("mood box only: status = %d, want 404", r.Code)
	}
	// And it is not on the app host.
	if r := getReply(t, e, appHost, "/style.css"); r.Code != http.StatusNotFound {
		t.Errorf("app host /style.css status = %d, want 404", r.Code)
	}
}

func TestPage_LinksTheOwnStylesheetOnlyWhenThereIsOne(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)

	page := string(getReply(t, e, anaHost, "/").Body)
	if strings.Contains(page, "style.css") {
		t.Errorf("page without a style links style.css: %.400s", page)
	}
	wantContains(t, page, `<body class="mood-none">`)

	e.publish(t, c, "none", with("c_bg", "#ff00cc"))
	page = string(getReply(t, e, anaHost, "/").Body)
	link := fmt.Sprintf(`<link rel="stylesheet" href="/style.css?v=%d">`, e.clock.Now().Unix())
	theme, own := strings.Index(page, `<link rel="stylesheet" href="/theme.css">`), strings.Index(page, link)
	if theme < 0 || own < theme {
		t.Errorf("theme at %d, style link at %d, want the style link after theme.css; head: %.500s", theme, own, page)
	}
	if strings.Contains(page, "own-mood-box") {
		t.Error("own-mood-box on <body> although the box keeps the mood colours")
	}
	if strings.Contains(page, "pv-bar") || strings.Contains(page, "noindex") {
		t.Error("the public page has the preview bar or noindex")
	}

	// A new publish is a new version, so a new URL.
	e.clock.Set(e.clock.Now().Add(90 * time.Second))
	e.publish(t, c, "none", with("c_bg", "#00ff00", "f_head", "comic"))
	page2 := string(getReply(t, e, anaHost, "/").Body)
	if strings.Contains(page2, link) || !strings.Contains(page2, fmt.Sprintf(`href="/style.css?v=%d"`, e.clock.Now().Unix())) {
		t.Errorf("the link did not move to the new version: %.600s", page2)
	}
}

func TestPage_OwnMoodBoxClassOnlyWhenSet(t *testing.T) {
	e := newPhotoEnv(t)
	c := e.signUp(t, "ana@example.com", "ana", false)
	wantStatus(t, e.form(t, c, "/mood", url.Values{"mood_key": {"fiesta"}}), http.StatusSeeOther)

	e.publish(t, c, "fiesta", without("own_mood_box"))
	page := string(getReply(t, e, anaHost, "/").Body)
	wantContains(t, page, `<body class="mood-fiesta own-mood-box">`)
	// A style with only the box set has no CSS, so no stylesheet to link.
	if strings.Contains(page, "style.css") {
		t.Error("page links style.css although the style sets no colour or font")
	}

	e.publish(t, c, "fiesta", with("c_bg", "#101010"))
	page = string(getReply(t, e, anaHost, "/").Body)
	wantContains(t, page, `<body class="mood-fiesta">`)
}
