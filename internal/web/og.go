package web

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/ogcard"
	"github.com/mayloo89/retratar/internal/user"
)

// ogPalette is parsed once at package init from the same theme.css every
// public page loads (see themeCSS in static.go), so the OG card can never
// silently drift from the page's own colours — see
// [ogcard.ParsePalette] and internal/ogcard/palette_test.go.
var ogPalette = mustParseOGPalette(themeCSS)

func mustParseOGPalette(css []byte) ogcard.Palette {
	p, err := ogcard.ParsePalette(css)
	if err != nil {
		panic(fmt.Sprintf("web: parse OG palette from theme.css: %v", err))
	}
	return p
}

// handleOGImage renders the Open Graph preview image for the handle
// resolved from the request's hostname — see [HandleFrom]. It follows
// handlePage's lookup-and-404 shape exactly: a card for a page that would
// 404 should also 404, not render an image for a handle nobody owns.
func (s *Server) handleOGImage(w http.ResponseWriter, r *http.Request) {
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

	card := ogcard.Card{Handle: u.Handle, MoodClass: "none"}

	var updatedAt time.Time
	m, err := s.Moods.CurrentMood(r.Context(), u.ID)
	switch {
	case err == nil:
		card.MoodClass = string(m.Key)
		card.MoodLabel = m.Key.Label()
		card.MoodNote = m.Note
		updatedAt = m.UpdatedAt
	case errors.Is(err, mood.ErrNoMood):
		// No mood set yet — card already carries the empty state.
	default:
		s.Logger.ErrorContext(r.Context(), "get current mood", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// The mood changes rarely, so a short public max-age plus a
	// content-derived ETag lets a repeat scrape or paste short-circuit to a
	// 304 before Render ever runs — the actual rasterisation is the
	// expensive part of this unauthenticated route.
	etag := ogETag(u.Handle, card.MoodClass, card.MoodNote, updatedAt)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	png, err := ogcard.Render(card, ogPalette)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "render og image", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	_, _ = w.Write(png)
}

// ogETag derives a strong ETag from everything that can change what Render
// draws. It is safe as a strong validator because Render is deterministic
// (see TestRender_IsDeterministic): the same inputs always produce the same
// PNG bytes, so a repaint of the same mood never invalidates a scraper's or
// a client's cache, and a mood change always does.
func ogETag(handle, moodClass, note string, updatedAt time.Time) string {
	sum := sha256.Sum256([]byte(handle + "\x00" + moodClass + "\x00" + note + "\x00" + updatedAt.UTC().Format(time.RFC3339Nano)))
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
