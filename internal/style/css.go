package style

import (
	"maps"
	"strings"
	"time"
)

// TextSoftProperty is the internal custom property theme.css derives from the
// text colour for secondary text. The editor does not show it.
const TextSoftProperty = "--s-text-soft"

// textSoft is how an owner's page derives secondary text when the owner sets
// Texto or Hoja: the text colour mixed with the sheet colour. 79% is the share
// that comes closest to theme.css's --ink-soft (oklch(0.42 0.012 90)) from its
// --ink (#292620) over its --paper (#fefdfa): the two differ by 0.003 in oklab
// distance, far below the 0.02 that is just noticeable.
const textSoft = "color-mix(in oklab, var(--s-text) 79%, var(--s-sheet))"

// Style is an owner's choices for their page: the colour slots and fonts they
// changed, and whether the "cómo estoy" box takes their colours. The zero
// Style is "everything default".
type Style struct {
	// Colors holds only the customised slots: slot key to lower-case #rrggbb.
	Colors map[string]string
	// HeadFont and BodyFont are registry keys, or "" for the default.
	HeadFont string
	BodyFont string
	// OwnMoodBox is true when the "cómo estoy" box takes the owner's colours
	// instead of keeping the mood's.
	OwnMoodBox bool
	// UpdatedAt is when the style was last published, or the zero time for
	// one that was never stored. It is not part of what the owner chooses:
	// [Style.Equal] ignores it.
	UpdatedAt time.Time
}

// IsDefault reports whether the style changes nothing, so a page with it is
// the same as a page with no style.
func (s Style) IsDefault() bool {
	return len(s.Colors) == 0 && s.HeadFont == "" && s.BodyFont == "" && !s.OwnMoodBox
}

// Equal reports whether two styles make the same choices.
func (s Style) Equal(o Style) bool {
	return maps.Equal(s.Colors, o.Colors) &&
		s.HeadFont == o.HeadFont && s.BodyFont == o.BodyFont &&
		s.OwnMoodBox == o.OwnMoodBox
}

// CSS returns the stylesheet that gives a page this style, or "" when it has
// nothing to say (no colour and no font set). OwnMoodBox is not in it: that is
// a class on <body>.
//
// It is built by hand, not with html/template, and needs no escaping, because
// every value in it is either a constant of this package (a property name, a
// font stack from the registry, the soft-text expression) or a colour that
// passed [ParseHex], which formats it itself. A colour that did not, a slot or a font the registry does
// not know, is skipped, never written. No user text reaches the output.
//
// Slots come out in [Slots] order, so equal styles give equal bytes.
func (s Style) CSS() string {
	var decls []string
	for _, slot := range slots {
		c, ok := parsed(s.Colors, slot.Key)
		if !ok {
			continue
		}
		decls = append(decls, slot.Property+": "+c+";")
	}
	if f, ok := FontByKey(s.HeadFont); ok {
		decls = append(decls, HeadFontProperty+": "+f.Stack+";")
	}
	if f, ok := FontByKey(s.BodyFont); ok {
		decls = append(decls, BodyFontProperty+": "+f.Stack+";")
	}
	if len(decls) == 0 {
		return ""
	}
	_, hasText := parsed(s.Colors, "text")
	_, hasSheet := parsed(s.Colors, "sheet")
	if hasText || hasSheet {
		decls = append(decls, TextSoftProperty+": "+textSoft+";")
	}

	var b strings.Builder
	b.WriteString("body {\n")
	for _, d := range decls {
		b.WriteString("\t" + d + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// parsed returns the colour of a slot as [ParseHex] formats it.
func parsed(colors map[string]string, key string) (string, bool) {
	c, ok := colors[key]
	if !ok {
		return "", false
	}
	return ParseHex(c)
}
