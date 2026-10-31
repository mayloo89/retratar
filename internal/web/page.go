package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"unicode"

	"github.com/mayloo89/retratar/internal/guestbook"
	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/ogcard"
	"github.com/mayloo89/retratar/internal/style"
	"github.com/mayloo89/retratar/internal/user"
)

// pageData is what page.html renders.
//
// MoodClass is always "none" or one of mood.AllKeys() — never raw user
// input. HasMood/MoodClass/MoodLabel come from the account's stored mood,
// whose key is validated by mood.Key.Valid at write time and constrained by
// the database's CHECK on moods.mood_key besides. That is what makes it safe
// to drop straight into the page's CSS class, no escaping needed beyond
// html/template's own auto-escaping.
type pageData struct {
	Handle string
	// Name is how the page identifies its owner: the display name if set,
	// otherwise the handle. User text, rendered only through html/template's
	// escaping.
	Name string
	// Initial is Name's first rune, upper-cased, for the framed initial in
	// the identity header. Display-only.
	Initial string
	// Address is the page's canonical host, without scheme, as shown under
	// the name.
	Address   string
	HasMood   bool
	MoodClass string
	MoodLabel string
	MoodNote  string

	// OGURL and OGImageURL are absolute — an og:image scrapers must resolve
	// without any base URL of their own to resolve it against. See
	// [config.Config.PageBaseURL].
	OGURL string
	// OGImageURL and OGDescription each feed the OG meta tags; page.html
	// also reuses OGDescription as the visible empty-state paragraph
	// ("Todavía no eligió...") rather than hardcoding that sentence a
	// second time — see [ogcard.EmptyStateText], its one source.
	OGImageURL    string
	OGDescription string

	// Guestbook is the newest visible entries, newest first. SignURL points
	// at the app surface: signing needs a session, which no page can see, so
	// the page only links out and never carries a form.
	Guestbook []guestbookEntryView
	SignURL   string

	// RandomURL points at /random on the app surface.
	RandomURL string

	// AppURL is the app home. Pages cannot see a session, so every visitor
	// gets the same link and the app decides where they belong. Absolute: a
	// relative "/" would resolve against the pages host.
	AppURL string

	// PrivacyURL and TermsURL point at the legal pages on the app surface,
	// absolute for the same reason SignURL is.
	PrivacyURL string
	TermsURL   string

	// PhotoURL is the owner's current photo as a same-origin path on this
	// page's own host, /photo/<key>.jpg, or empty when there is none. It is
	// never the app-host URL: that one is owner-only and no-store.
	// PhotoWidth and PhotoHeight are the stored size, for <img width height>.
	PhotoURL    string
	PhotoWidth  int
	PhotoHeight int

	// OwnMoodBox adds the own-mood-box class to <body>: the "cómo estoy" box
	// takes the owner's colours. The class is a constant in the template.
	OwnMoodBox bool
	// StyleVersion is the owner's style stylesheet version: updated_at, as read
	// back from the database, in Unix microseconds. Postgres keeps microseconds,
	// so two publishes never share a version. Zero means the page has no
	// stylesheet of its own. Only a number ever reaches the link's query.
	StyleVersion int64

	// Preview is set only by the owner's preview on the app host. The page then
	// links PreviewCSSURL (the draft, as a same-origin URL built from validated
	// values) and /preview.css instead of /style.css, is marked noindex, and
	// starts with a bar whose form carries PreviewFields, the whole draft.
	Preview       bool
	PreviewCSSURL string
	PreviewFields []hiddenField
}

// hiddenField is one hidden input of the preview bar.
type hiddenField struct{ Name, Value string }

// guestbookEntryView is one guestbook entry as page.html renders it. Body is
// plain text; html/template escapes it and CSS shows its line breaks.
type guestbookEntryView struct {
	AuthorHandle string
	AuthorURL    string
	Date         string
	Body         string
}

// guestbookDateLayout is day/month/year, the order readers of the es-AR
// pages expect.
const guestbookDateLayout = "02/01/2006"

// argentina is UTC-3 all year (no DST since 2009). A fixed zone avoids
// depending on tzdata in the distroless image.
var argentina = time.FixedZone("ART", -3*60*60)

// handlePage renders the public page for the handle resolved from the
// request's hostname; see [HandleFrom].
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	handle, ok := HandleFrom(r.Context())
	if !ok {
		http.NotFound(w, r)
		return
	}

	u, err := s.Users.GetByHandle(r.Context(), handle)
	if err != nil {
		if errors.Is(err, user.ErrUserNotFound) {
			http.NotFound(w, r)
			return
		}
		s.Logger.ErrorContext(r.Context(), "get user by handle", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// A style that fails to load must not take the page down with it: log it
	// and render the page as if it had none. This is one small query on top of
	// the user's; folding it into GetByHandle would make every user lookup
	// pay for it.
	st, err := s.Users.GetStyle(r.Context(), u.ID)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "get page style", slog.String("error", err.Error()))
		st = style.Style{}
	}

	data, err := s.pageFor(r.Context(), u, st)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.renderTemplate(w, http.StatusOK, "page.html", data)
}

// pageFor builds what page.html renders for u with the style st: the public
// page passes the stored style, the owner's preview passes a draft. It logs
// what it cannot load and returns an error only for a failure the caller
// should answer with a 500.
func (s *Server) pageFor(ctx context.Context, u user.User, st style.Style) (pageData, error) {
	base := s.Config.PageBaseURL(u.Handle)
	data := pageData{
		Handle:        u.Handle,
		Name:          u.ShownName(),
		Initial:       initialOf(u.ShownName()),
		Address:       s.Config.PageHostFor(u.Handle),
		MoodClass:     "none",
		OGURL:         base + "/",
		OGImageURL:    base + "/og.png",
		OGDescription: ogcard.EmptyStateText,
		SignURL:       s.Config.BaseURL() + "/sign/" + u.Handle,
		RandomURL:     s.Config.BaseURL() + "/random",
		AppURL:        s.Config.BaseURL() + "/",
		PrivacyURL:    s.Config.BaseURL() + "/privacy",
		TermsURL:      s.Config.BaseURL() + "/terms",
		OwnMoodBox:    st.OwnMoodBox,
	}
	// A style with no colour and no font has no stylesheet to link.
	if st.CSS() != "" {
		data.StyleVersion = st.UpdatedAt.UnixMicro()
	}

	if u.PhotoKey != "" {
		data.PhotoURL = photoURL(u.PhotoKey)
		data.PhotoWidth, data.PhotoHeight = u.PhotoWidth, u.PhotoHeight
	}

	m, err := s.Moods.CurrentMood(ctx, u.ID)
	switch {
	case err == nil:
		data.HasMood = true
		data.MoodClass = string(m.Key)
		data.MoodLabel = m.Key.Label()
		data.MoodNote = m.Note
		data.OGDescription = m.Key.Label()
		if m.Note != "" {
			data.OGDescription += ": " + m.Note
		}
	case errors.Is(err, mood.ErrNoMood):
		// No mood set yet — data already carries the empty state.
	default:
		s.Logger.ErrorContext(ctx, "get current mood", slog.String("error", err.Error()))
		return pageData{}, err
	}

	// A guestbook that fails to load must not take the mood down with it: log
	// it and render the page without entries.
	entries, err := s.Guestbook.Visible(ctx, u.ID, guestbook.PageLimit)
	if err != nil {
		s.Logger.ErrorContext(ctx, "list guestbook entries", slog.String("error", err.Error()))
	}
	for _, e := range entries {
		data.Guestbook = append(data.Guestbook, guestbookEntryView{
			AuthorHandle: e.AuthorHandle,
			AuthorURL:    s.Config.PageBaseURL(e.AuthorHandle) + "/",
			Date:         e.CreatedAt.In(argentina).Format(guestbookDateLayout),
			Body:         e.Body,
		})
	}
	return data, nil
}

// handleStyleCSS serves the page's own stylesheet: the owner's colours and
// fonts, built from the stored, validated style. A handle that does not exist,
// or a page with nothing to say, is a 404.
//
// page.html links it with ?v=<updated_at>, and a new version is a new URL, so
// a request that carries v is cacheable for good. The handler ignores v's
// value. Without v there is no way to bust the cache, so that response is not
// cached.
func (s *Server) handleStyleCSS(w http.ResponseWriter, r *http.Request) {
	handle, ok := HandleFrom(r.Context())
	if !ok {
		http.NotFound(w, r)
		return
	}
	u, err := s.Users.GetByHandle(r.Context(), handle)
	if err != nil {
		if errors.Is(err, user.ErrUserNotFound) {
			http.NotFound(w, r)
			return
		}
		s.Logger.ErrorContext(r.Context(), "get user by handle", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	st, err := s.Users.GetStyle(r.Context(), u.ID)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "get page style", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	css := st.CSS()
	if css == "" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	if r.URL.Query().Has("v") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = w.Write([]byte(css))
}

// handleTheme serves the one stylesheet every public page loads.
func (s *Server) handleTheme(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(themeCSS)
}

// handleAppCSS serves the stylesheet every app-surface screen loads. It is
// handleTheme's twin for the app host.
func (s *Server) handleAppCSS(w http.ResponseWriter, _ *http.Request) {
	// PrivateCache has already set no-store; this Set overrides it, on purpose:
	// the stylesheet is the same for everyone and safe to cache.
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(appCSS)
}

// handleFont serves one self-hosted font on either host: a page loads its fonts
// from its own host, which is all font-src 'self' allows. The file name holds a
// hash of the content, so the response is cacheable for good. An unknown name,
// including any path trick, is a 404.
func (s *Server) handleFont(w http.ResponseWriter, r *http.Request) {
	data, ok := fontFiles[r.PathValue("file")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	// On the app host PrivateCache has already set no-store; this Set
	// overrides it, on purpose, as handleAppCSS does.
	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

// initialOf returns the first rune of s, upper-cased. A rune, not a byte, so
// it stays right for display names, which are not ASCII.
func initialOf(s string) string {
	for _, r := range s {
		return string(unicode.ToUpper(r))
	}
	return ""
}
