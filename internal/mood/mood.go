// Package mood owns the mood a user's page currently shows.
//
// A mood is a fixed key — one of a small, closed vocabulary, never free text
// — plus an optional short note. The key is what a theme's CSS keys off of;
// the note is the away-message part.
package mood

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Key is one of the fixed mood vocabulary. It is never built from user input
// directly: a value only ever comes from [ParseKey], which validates against
// the vocabulary, or from a database row whose mood_key column carries the
// same CHECK constraint. That is what makes it safe to drop straight into a
// CSS class name when rendering a page.
type Key string

// The fixed mood vocabulary. Display labels are Spanish; keys are ASCII so
// they double as CSS class names and HTML form values without escaping.
const (
	KeyFeliz      Key = "feliz"
	KeyTriste     Key = "triste"
	KeyTranquilo  Key = "tranquilo"
	KeyAnsioso    Key = "ansioso"
	KeyEnamorado  Key = "enamorado"
	KeyCansado    Key = "cansado"
	KeyEnojado    Key = "enojado"
	KeyAburrido   Key = "aburrido"
	KeyInspirado  Key = "inspirado"
	KeyNostalgico Key = "nostalgico"
	KeyFiesta     Key = "fiesta"
	KeyPerdido    Key = "perdido"
)

// labels maps each key to the text a page shows. Keeping this as the single
// switch every other lookup goes through means adding a mood later is one
// place, not several.
var labels = map[Key]string{
	KeyFeliz:      "feliz",
	KeyTriste:     "triste",
	KeyTranquilo:  "tranquilo",
	KeyAnsioso:    "ansioso",
	KeyEnamorado:  "enamorado",
	KeyCansado:    "cansado",
	KeyEnojado:    "enojado",
	KeyAburrido:   "aburrido",
	KeyInspirado:  "inspirado",
	KeyNostalgico: "nostálgico",
	KeyFiesta:     "de fiesta",
	KeyPerdido:    "perdido",
}

// AllKeys returns every valid key, in the fixed display order — for
// rendering a picker.
func AllKeys() []Key {
	return []Key{
		KeyFeliz, KeyTriste, KeyTranquilo, KeyAnsioso, KeyEnamorado, KeyCansado,
		KeyEnojado, KeyAburrido, KeyInspirado, KeyNostalgico, KeyFiesta, KeyPerdido,
	}
}

// Valid reports whether k is one of the fixed vocabulary.
func (k Key) Valid() bool {
	_, ok := labels[k]
	return ok
}

// Label returns the Spanish text a page shows for k, or "" if k is not
// [Valid].
func (k Key) Label() string {
	return labels[k]
}

// maxNoteRunes is the note's length limit, counted in runes rather than
// bytes so an accented character costs the same as a plain one.
const maxNoteRunes = 60

// NormaliseNote trims a note and enforces the length and shape a page can
// show on one line. Empty is valid — the note is optional, the mood key
// alone is a complete mood.
func NormaliseNote(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if utf8.RuneCountInString(trimmed) > maxNoteRunes {
		return "", ErrInvalidNote
	}
	// A note is an away-message line, not a message: rejecting control
	// characters (line breaks included) keeps it to one line wherever it is
	// rendered, without the renderer having to strip anything itself.
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return "", ErrInvalidNote
		}
	}
	return trimmed, nil
}

// Mood is the state a page currently shows.
type Mood struct {
	UserID    uuid.UUID
	Key       Key
	Note      string
	UpdatedAt time.Time
}
