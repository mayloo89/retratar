// Package textcheck is the one check for text a user typed that a page will
// show: the display name, the mood note and the guestbook body all use it, so
// they reject the same invisible and spoofing characters.
package textcheck

import (
	"unicode"
	"unicode/utf8"
)

// Line reports whether s is acceptable single-line user text: valid UTF-8,
// no control characters, and no invisible format or separator characters
// (see disallowedFormat).
func Line(s string) bool {
	return check(s, false)
}

// Block is Line, but also allows '\n'.
func Block(s string) bool {
	return check(s, true)
}

func check(s string, allowNewline bool) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '\n' && allowNewline {
			continue
		}
		if unicode.IsControl(r) || disallowedFormat(r) {
			return false
		}
	}
	return true
}

// disallowedFormat reports whether r is an invisible Unicode format or
// separator character that unicode.IsControl misses but a display name must
// not contain.
//
// The bidi embedding, override and isolate controls (U+202A–U+202E,
// U+2066–U+2069) and the bidi marks (U+200E, U+200F) change how the text
// around them is ordered. A right-to-left override can reverse what follows
// it, so a name could be made to look like another name on the page, in
// <title> and in link previews. U+2028 and U+2029 are line and paragraph
// separators: line breaks that get past the "one line" rule.
//
// U+200D (zero-width joiner) stays allowed on purpose: multi-part emoji such
// as families and skin-tone sequences cannot be written without it.
func disallowedFormat(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x200E, r == 0x200F, r == 0x2028, r == 0x2029:
		return true
	}
	return false
}
