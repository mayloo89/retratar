package web

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/style"
	"github.com/mayloo89/retratar/internal/user"
)

// msgInvalidStyle is shown in the "Tu estilo" box when the form could not be
// read as a style. The stored style is kept.
const msgInvalidStyle = "No pudimos guardar ese estilo. Probá de nuevo."

// Form and preview-URL field names. The dashboard form and the preview bar's
// hidden inputs use the same ones, and so do the keys of /style/preview.css.
const (
	fieldColorPrefix = "c_"
	fieldHeadFont    = "f_head"
	fieldBodyFont    = "f_body"
	fieldOwnMoodBox  = "own_mood_box"
	// previewOwn is the preview URL's flag: "1" when the box takes the owner's
	// colours. The form says it the other way round; see parseDraft.
	previewOwn = "own"

	// resetHead and resetBody are what a font row's Restablecer button sends;
	// a colour row sends its slot key.
	resetHead = "head"
	resetBody = "body"
)

// currentMoodKey is the key whose defaults the owner's draft is measured
// against: their current mood, or "none" before they have set one.
func (s *Server) currentMoodKey(r *http.Request, u user.User) (string, error) {
	m, err := s.Moods.CurrentMood(r.Context(), u.ID)
	switch {
	case err == nil:
		return string(m.Key), nil
	case errors.Is(err, mood.ErrNoMood):
		return "none", nil
	}
	return "", err
}

// parseDraft reads the "Tu estilo" form into a draft style. It returns false
// for input that is not a style: a missing field, a colour that is not
// #rrggbb, a font that is not in the registry.
//
// A native colour input cannot be empty, so an untouched slot submits the
// colour it shows, which is its default under the current mood. A value equal
// to that default is therefore "Por defecto", not a customised slot; without
// this every slot would turn to "Personalizado" after the first publish. Fonts
// follow the same rule against the default font's key. reset names the slot
// whose Restablecer was pressed ("" for none): its value is not read, the slot
// is default.
//
// The checkbox is the other way round from the field it sets. Checked means
// the "cómo estoy" box keeps the mood colours, which is the default, and an
// unchecked checkbox is not sent at all. So the field being present means
// OwnMoodBox is false, and absent means it is true.
func parseDraft(form url.Values, moodKey, reset string) (style.Style, bool) {
	var st style.Style
	for _, slot := range style.Slots() {
		if reset == slot.Key {
			continue
		}
		raw, ok := form[fieldColorPrefix+slot.Key]
		if !ok || len(raw) != 1 {
			return style.Style{}, false
		}
		hex, ok := style.ParseHex(raw[0])
		if !ok {
			return style.Style{}, false
		}
		if hex == style.Default(slot.Key, moodKey) {
			continue
		}
		if st.Colors == nil {
			st.Colors = map[string]string{}
		}
		st.Colors[slot.Key] = hex
	}

	var ok bool
	if st.HeadFont, ok = parseFont(form, fieldHeadFont, style.DefaultHeadFont, reset == resetHead); !ok {
		return style.Style{}, false
	}
	if st.BodyFont, ok = parseFont(form, fieldBodyFont, style.DefaultBodyFont, reset == resetBody); !ok {
		return style.Style{}, false
	}

	_, keepsMood := form[fieldOwnMoodBox]
	st.OwnMoodBox = !keepsMood
	return st, true
}

// parseFont reads one font select. The result is "" for the default.
func parseFont(form url.Values, field, defaultKey string, reset bool) (string, bool) {
	if reset {
		return "", true
	}
	raw, ok := form[field]
	if !ok || len(raw) != 1 {
		return "", false
	}
	if _, known := style.FontByKey(raw[0]); !known {
		return "", false
	}
	if raw[0] == defaultKey {
		return "", true
	}
	return raw[0], true
}

// validReset reports whether v is a slot or font a Restablecer button can name.
func validReset(v string) bool {
	if v == resetHead || v == resetBody {
		return true
	}
	for _, slot := range style.Slots() {
		if v == slot.Key {
			return true
		}
	}
	return false
}

// handleStyleSubmit takes the "Tu estilo" form. Its buttons are:
//
//	reset=<slot>      clear one slot in the draft, show the dashboard
//	action=preview    show the real page with the draft
//	action=edit       the preview's "Seguir cambiando": the dashboard, draft kept
//	action=publish    store the draft
//
// Everything but publish stores nothing; the draft travels in the form. Every
// field is read the same way whichever button sent it, so the preview's hidden
// inputs are trusted no more than the visible ones.
func (s *Server) handleStyleSubmit(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	// A style belongs to a page, and a page to a handle.
	if u.State != user.StateActive {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	reset := r.PostForm.Get("reset")
	action := r.PostForm.Get("action")
	switch {
	case reset != "":
		if !validReset(reset) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
	case action != "preview" && action != "edit" && action != "publish":
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	moodKey, err := s.currentMoodKey(r, u)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "get current mood", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	draft, ok := parseDraft(r.PostForm, moodKey, reset)
	if !ok {
		s.renderAppHomeMessages(w, r, http.StatusUnprocessableEntity, u, homeMessages{Style: msgInvalidStyle})
		return
	}

	switch {
	case reset != "" || action == "edit":
		s.renderAppHomeMessages(w, r, http.StatusOK, u, homeMessages{Draft: &draft})
	case action == "preview":
		s.renderStylePreview(w, r, u, draft, moodKey)
	default:
		if err := s.Users.SetStyle(r.Context(), u.ID, draft, s.now()); err != nil {
			if errors.Is(err, user.ErrInvalidStyle) {
				s.renderAppHomeMessages(w, r, http.StatusUnprocessableEntity, u, homeMessages{Style: msgInvalidStyle})
				return
			}
			s.Logger.ErrorContext(r.Context(), "set page style", slog.String("error", err.Error()))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

// renderStylePreview renders the owner's real page with the draft, on the app
// host. The stylesheets all come from this host because style-src is 'self',
// and the photo comes through /photo/{file} on it for the same reason.
func (s *Server) renderStylePreview(w http.ResponseWriter, r *http.Request, u user.User, draft style.Style, moodKey string) {
	data, err := s.pageFor(r.Context(), u, draft)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data.StyleVersion = 0
	data.Preview = true
	data.PreviewCSSURL = "/style/preview.css?" + previewQuery(draft).Encode()
	data.PreviewFields = previewFields(draft, moodKey)
	s.renderTemplate(w, http.StatusOK, "page.html", data)
}

// previewQuery is the draft as /style/preview.css reads it: the customised
// slots only.
func previewQuery(st style.Style) url.Values {
	q := url.Values{}
	for _, slot := range style.Slots() {
		if c, ok := st.Colors[slot.Key]; ok {
			q.Set(fieldColorPrefix+slot.Key, c)
		}
	}
	if st.HeadFont != "" {
		q.Set(fieldHeadFont, st.HeadFont)
	}
	if st.BodyFont != "" {
		q.Set(fieldBodyFont, st.BodyFont)
	}
	if st.OwnMoodBox {
		q.Set(previewOwn, "1")
	}
	return q
}

// previewFields is the draft as the dashboard form would send it, one hidden
// input per field, so the bar's buttons post exactly what parseDraft reads.
// A default slot carries its default under the current mood, as the colour
// input beside it would.
func previewFields(st style.Style, moodKey string) []hiddenField {
	var fields []hiddenField
	for _, slot := range style.Slots() {
		v, ok := st.Colors[slot.Key]
		if !ok {
			v = style.Default(slot.Key, moodKey)
		}
		fields = append(fields, hiddenField{fieldColorPrefix + slot.Key, v})
	}
	fields = append(fields,
		hiddenField{fieldHeadFont, fontOrDefault(st.HeadFont, style.DefaultHeadFont)},
		hiddenField{fieldBodyFont, fontOrDefault(st.BodyFont, style.DefaultBodyFont)},
	)
	if !st.OwnMoodBox {
		fields = append(fields, hiddenField{fieldOwnMoodBox, "no"})
	}
	return fields
}

func fontOrDefault(key, def string) string {
	if key == "" {
		return def
	}
	return key
}

// handleStylePreviewCSS serves the stylesheet of a draft, for the preview. It
// reads the draft from the query string, validates it as strictly as the
// database would, and builds the CSS with the same function the page's own
// stylesheet uses. It touches no database and is never cached. Any invalid
// value is a 400.
func (s *Server) handleStylePreviewCSS(w http.ResponseWriter, r *http.Request) {
	st, ok := styleFromQuery(r.URL.Query())
	if !ok {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(st.CSS()))
}

// styleFromQuery is the inverse of previewQuery. Parameters it does not know
// are ignored; the ones it knows must be valid and appear once.
func styleFromQuery(q url.Values) (style.Style, bool) {
	var st style.Style
	for _, slot := range style.Slots() {
		vals, present := q[fieldColorPrefix+slot.Key]
		if !present {
			continue
		}
		if len(vals) != 1 {
			return style.Style{}, false
		}
		hex, ok := style.ParseHex(vals[0])
		if !ok {
			return style.Style{}, false
		}
		if st.Colors == nil {
			st.Colors = map[string]string{}
		}
		st.Colors[slot.Key] = hex
	}
	for _, f := range []struct {
		field string
		dst   *string
	}{{fieldHeadFont, &st.HeadFont}, {fieldBodyFont, &st.BodyFont}} {
		vals, present := q[f.field]
		if !present {
			continue
		}
		if len(vals) != 1 {
			return style.Style{}, false
		}
		if _, known := style.FontByKey(vals[0]); !known {
			return style.Style{}, false
		}
		*f.dst = vals[0]
	}
	if vals, present := q[previewOwn]; present {
		if len(vals) != 1 || vals[0] != "1" {
			return style.Style{}, false
		}
		st.OwnMoodBox = true
	}
	return st, true
}

// handlePreviewCSS serves the preview bar's stylesheet, on the app host.
func (s *Server) handlePreviewCSS(w http.ResponseWriter, _ *http.Request) {
	// PrivateCache has already set no-store; this Set overrides it, on purpose,
	// as handleAppCSS does: the file is the same for everyone.
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(previewCSS)
}
