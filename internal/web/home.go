package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mayloo89/retratar/internal/guestbook"
	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/style"
	"github.com/mayloo89/retratar/internal/user"
)

type moodOption struct {
	Key      string
	Label    string
	Selected bool
}

// appHomeData is what app_home.html renders.
type appHomeData struct {
	// MoodClass is the owner's current mood key, or empty when none is set
	// (no class, so the stylesheet's warm default applies). Always a
	// mood.Key, never request input.
	MoodClass   string
	Handle      string
	DisplayName string
	NameError   string
	PageURL     string
	PageAddress string
	HasMood     bool
	MoodLabel   string
	MoodNote    string
	MoodOptions []moodOption
	Error       string

	// CanUploadPhoto shows the "Tu foto" box; only allowlisted accounts see
	// it. PhotoURL is the app-host path of the current photo, empty when
	// there is none, and PhotoWidth/PhotoHeight its size.
	CanUploadPhoto    bool
	PhotoURL          string
	PhotoWidth        int
	PhotoHeight       int
	PhotoChangedToday bool
	PhotoError        string

	// Guestbook is the owner's entries of any state, newest first.
	Guestbook []ownEntryView

	// Style is the "Tu estilo" box.
	Style styleBox
}

// styleBox is what the "Tu estilo" box renders: the draft the form shows,
// which is the stored style unless a reset or "Seguir cambiando" brought one.
type styleBox struct {
	Colors []styleColorRow
	Fonts  []styleFontRow
	// KeepsMoodBox is the checkbox: checked when "cómo estoy" keeps the mood
	// colours, which is when the draft's OwnMoodBox is false.
	KeepsMoodBox bool
	// Unsaved is set when the draft differs from what is stored.
	Unsaved bool
	Error   string
}

// styleColorRow is one colour slot. Hex is the draft's value, or the slot's
// default under the current mood when the slot is not customised.
type styleColorRow struct {
	Key, Label, Hex string
	Custom          bool
}

// styleFontRow is one typeface select. Field is the select's name and Reset
// what its Restablecer button sends.
type styleFontRow struct {
	Field, Reset, Label string
	Custom              bool
	Options             []fontOption
}

type fontOption struct {
	Key, Label string
	Selected   bool
}

// styleBoxFor builds the box for the draft, against the mood whose defaults
// the uncustomised slots show.
func styleBoxFor(draft, stored style.Style, moodKey, errMsg string) styleBox {
	box := styleBox{KeepsMoodBox: !draft.OwnMoodBox, Unsaved: !draft.Equal(stored), Error: errMsg}
	for _, slot := range style.Slots() {
		row := styleColorRow{Key: slot.Key, Label: slot.Label, Hex: style.Default(slot.Key, moodKey)}
		if c, ok := draft.Colors[slot.Key]; ok {
			row.Hex, row.Custom = c, true
		}
		box.Colors = append(box.Colors, row)
	}
	fontRow := func(field, reset, label, chosen, defaultKey string) styleFontRow {
		row := styleFontRow{Field: field, Reset: reset, Label: label, Custom: chosen != ""}
		selected := fontOrDefault(chosen, defaultKey)
		for _, f := range style.Fonts() {
			row.Options = append(row.Options, fontOption{Key: f.Key, Label: f.Label, Selected: f.Key == selected})
		}
		return row
	}
	box.Fonts = []styleFontRow{
		fontRow(fieldHeadFont, resetHead, "Tipografía de los títulos", draft.HeadFont, style.DefaultHeadFont),
		fontRow(fieldBodyFont, resetBody, "Tipografía del texto", draft.BodyFont, style.DefaultBodyFont),
	}
	return box
}

// ownEntryView is one guestbook entry as the owner's dashboard renders it.
// Body is plain text; html/template escapes it.
type ownEntryView struct {
	ID           string
	AuthorHandle string
	AuthorURL    string
	Date         string
	Body         string
	Hidden       bool
}

// handleAppHome is the signed-in landing page. An anonymous visitor is sent
// to sign in. An account with no handle yet sees the claim form instead of
// the dashboard — there is no page URL to show until one is claimed.
func (s *Server) handleAppHome(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if u.State != user.StateActive {
		s.renderTemplate(w, http.StatusOK, "claim_handle.html", claimHandleData{})
		return
	}

	s.renderAppHome(w, r, http.StatusOK, u, "")
}

// renderAppHome loads the account's current mood and renders the dashboard.
// errMsg, if non-empty, is shown alongside the mood form; [handleMoodSubmit]
// uses this with a non-200 status to redisplay the form after a rejected
// submission, the same shape handleLoginRequest uses for login_form.html.
func (s *Server) renderAppHome(w http.ResponseWriter, r *http.Request, status int, u user.User, errMsg string) {
	s.renderAppHomeErrors(w, r, status, u, errMsg, "")
}

// renderAppHomeErrors is [Server.renderAppHome] with a second message, shown
// next to the name form instead of the mood form. [handleNameSubmit] passes
// u with the rejected text in DisplayName so the field keeps what was typed.
func (s *Server) renderAppHomeErrors(w http.ResponseWriter, r *http.Request, status int, u user.User, errMsg, nameErr string) {
	s.renderAppHomeMessages(w, r, status, u, homeMessages{Mood: errMsg, Name: nameErr})
}

// homeMessages are the per-form error messages the dashboard can show; each
// appears next to its own form.
type homeMessages struct {
	Mood, Name, Photo, Style string
	// Draft is a style the owner has not published, to show in the "Tu estilo"
	// box in place of the stored one. Nil means the stored style.
	Draft *style.Style
}

// renderAppHomeMessages is the dashboard renderer the others delegate to.
func (s *Server) renderAppHomeMessages(w http.ResponseWriter, r *http.Request, status int, u user.User, msgs homeMessages) {
	data := appHomeData{
		Handle: u.Handle, DisplayName: u.DisplayName,
		NameError: msgs.Name, Error: msgs.Mood, PhotoError: msgs.Photo,
		CanUploadPhoto: u.CanUploadPhotos,
	}
	if u.PhotoKey != "" {
		data.PhotoURL = photoURL(u.PhotoKey)
		data.PhotoWidth, data.PhotoHeight = u.PhotoWidth, u.PhotoHeight
	}
	data.PhotoChangedToday = u.UploadedPhotoOn(s.now().In(argentina))
	// An account without a handle has no page yet; POST /mood can still
	// re-render this dashboard for one, so leave PageURL empty for it.
	if u.Handle != "" {
		data.PageURL = s.Config.PageBaseURL(u.Handle)
		data.PageAddress = s.Config.PageHostFor(u.Handle)
	}

	m, err := s.Moods.CurrentMood(r.Context(), u.ID)
	switch {
	case err == nil:
		data.HasMood = true
		data.MoodClass = string(m.Key)
		data.MoodLabel = m.Key.Label()
		data.MoodNote = m.Note
	case errors.Is(err, mood.ErrNoMood):
		// No mood set yet — data already carries the empty state.
	default:
		s.Logger.ErrorContext(r.Context(), "get current mood", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	for _, k := range mood.AllKeys() {
		data.MoodOptions = append(data.MoodOptions, moodOption{
			Key: string(k), Label: k.Label(), Selected: data.HasMood && k == m.Key,
		})
	}

	// A guestbook that fails to load must not take the dashboard down with
	// it: log it and render the dashboard without the section. An account
	// without a handle has no page, hence no guestbook.
	if u.Handle != "" {
		entries, err := s.Guestbook.Own(r.Context(), u.ID, guestbook.OwnerLimit)
		if err != nil {
			s.Logger.ErrorContext(r.Context(), "list own guestbook entries", slog.String("error", err.Error()))
		}
		for _, e := range entries {
			data.Guestbook = append(data.Guestbook, ownEntryView{
				ID:           e.ID.String(),
				AuthorHandle: e.AuthorHandle,
				AuthorURL:    s.Config.PageBaseURL(e.AuthorHandle) + "/",
				Date:         e.CreatedAt.In(argentina).Format(guestbookDateLayout),
				Body:         e.Body,
				Hidden:       e.Hidden,
			})
		}
	}

	// The box shows the draft if there is one, and the stored style otherwise.
	// Its uncustomised slots show what they default to under the current mood.
	// A style that fails to load takes the dashboard down: showing defaults
	// would invite a publish that overwrites what is stored.
	if u.Handle != "" {
		stored, err := s.Users.GetStyle(r.Context(), u.ID)
		if err != nil {
			s.Logger.ErrorContext(r.Context(), "get page style", slog.String("error", err.Error()))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		draft := stored
		if msgs.Draft != nil {
			draft = *msgs.Draft
		}
		moodKey := "none"
		if data.HasMood {
			moodKey = data.MoodClass
		}
		data.Style = styleBoxFor(draft, stored, moodKey, msgs.Style)
	}

	s.renderTemplate(w, status, "app_home.html", data)
}
