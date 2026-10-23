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

// disallowedFormat reports whether r is an invisible Unicode character that
// unicode.IsControl misses but user text must not contain: every format
// character (category Cf), plus the line and paragraph separators U+2028 and
// U+2029 (categories Zl and Zp), which are line breaks that get past the "one
// line" rule.
//
// The rule is by category rather than a list of code points because lists go
// stale. Cf holds the bidi embedding, override and isolate controls
// (U+202A–U+202E, U+2066–U+2069), the bidi marks (U+200E, U+200F, U+061C) and
// zero-width characters (U+200B, U+2060, U+FEFF), and soft hyphen (U+00AD).
// A right-to-left override can reverse what follows it, and zero-width
// characters make two different names look identical; on a page, in <title>
// and in link previews.
//
// Three exceptions stay allowed, because text cannot be written without them:
//   - U+200D (zero-width joiner): multi-part emoji such as families and
//     skin-tone sequences.
//   - U+200C (zero-width non-joiner): needed to spell words correctly in some
//     languages, such as Persian.
//   - U+E0020–U+E007F (tag characters): subdivision flag emoji, such as
//     Scotland's.
func disallowedFormat(r rune) bool {
	switch {
	case r == 0x200D, r == 0x200C, r >= 0xE0020 && r <= 0xE007F:
		return false
	case r == 0x2028, r == 0x2029:
		return true
	}
	return unicode.Is(unicode.Cf, r)
}
