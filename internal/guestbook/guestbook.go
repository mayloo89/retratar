// Package guestbook owns the entries other people leave on a user's page.
package guestbook

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// PageLimit is how many entries a public page shows.
const PageLimit = 20

// maxBodyRunes is the entry's length limit, counted in runes rather than
// bytes so an accented character costs the same as a plain one. It matches
// the CHECK on guestbook_entries.body.
const maxBodyRunes = 280

// NormaliseBody trims an entry, normalises its line endings and enforces the
// length and shape a page can show.
//
// Unlike a mood note (see mood.NormaliseNote), an entry may span several
// lines: it is a message, not an away-message line. Line breaks are kept as
// plain "\n" and the page renders them with CSS, so the body never needs any
// markup. Every other control character is rejected.
func NormaliseBody(raw string) (string, error) {
	body := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	// RuneCountInString counts each invalid byte as a rune, so invalid UTF-8
	// would pass the length check and then fail the insert as a 500.
	if !utf8.ValidString(body) {
		return "", ErrInvalidBody
	}
	n := utf8.RuneCountInString(body)
	if n == 0 || n > maxBodyRunes {
		return "", ErrInvalidBody
	}
	for _, r := range body {
		if r != '\n' && unicode.IsControl(r) {
			return "", ErrInvalidBody
		}
	}
	return body, nil
}

// Entry is one visible guestbook entry, with its author's handle.
type Entry struct {
	ID           uuid.UUID
	AuthorHandle string
	Body         string
	CreatedAt    time.Time
}
