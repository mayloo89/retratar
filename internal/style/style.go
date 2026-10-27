// Package style is the Go side of the page theme's slots: which colours an
// owner can set, what each one defaults to for every mood, and which fonts
// they can pick.
//
// It mirrors internal/web/static/theme.css and knows nothing about HTTP or
// the database. Tests in internal/web keep it in sync with the stylesheet: a
// slot, a mood colour or a font file that changes in one place and not the
// other fails the build.
package style

import (
	"regexp"
	"strings"
)

// Slot is one colour an owner can set. The seven slots are the only colour
// knobs a page has.
type Slot struct {
	// Key is the name used in forms and URLs.
	Key string
	// Property is the CSS custom property theme.css reads for it.
	Property string
	// Label is the Spanish name the editor shows.
	Label string
}

var slots = []Slot{
	{Key: "bg", Property: "--s-bg", Label: "Fondo"},
	{Key: "sheet", Property: "--s-sheet", Label: "Hoja"},
	{Key: "text", Property: "--s-text", Label: "Texto"},
	{Key: "link", Property: "--s-link", Label: "Links"},
	{Key: "name", Property: "--s-name", Label: "Nombre"},
	{Key: "barbg", Property: "--s-bar-bg", Label: "Barras de título"},
	{Key: "barfg", Property: "--s-bar-fg", Label: "Texto de las barras"},
}

// Slots returns the colour slots in the order the editor shows them.
func Slots() []Slot {
	return append([]Slot(nil), slots...)
}

// The font slots are not colours, so they are not in [Slots]; these are the
// custom properties theme.css reads for them.
const (
	HeadFontProperty = "--s-font-head"
	BodyFontProperty = "--s-font-body"
)

// moodColors is a mood's tint pair, as theme.css's .mood-<key> rules have it.
type moodColors struct{ bg, fg string }

// moods holds the 12 moods plus "none", the page of someone who has not set
// one yet.
var moods = map[string]moodColors{
	"feliz":      {"#fff4cc", "#5c4a00"},
	"triste":     {"#dbe6f2", "#22344a"},
	"tranquilo":  {"#dff2ea", "#1f4a3a"},
	"ansioso":    {"#fde3d0", "#5c2e00"},
	"enamorado":  {"#fbdce6", "#5c1f34"},
	"cansado":    {"#e6e2f2", "#382f5c"},
	"enojado":    {"#f6d3d3", "#5c1a1a"},
	"aburrido":   {"#ececec", "#3a3a3a"},
	"inspirado":  {"#d9f2d0", "#23491a"},
	"nostalgico": {"#f0e0cc", "#4a3620"},
	"fiesta":     {"#f2d0f2", "#4a1a4a"},
	"perdido":    {"#d6d6d6", "#2b2b2b"},
	"none":       {"#f4f4f4", "#222222"},
}

// Paper and ink are theme.css's --paper and --ink, in hex.
const (
	paper = "#fefdfa"
	ink   = "#292620"
)

// Default returns the colour a slot has when the owner has not set it: the
// one theme.css gives it under the mood, as lower-case #rrggbb. It returns ""
// for a slot or mood key it does not know. The mood key "none" is the page
// with no mood.
func Default(slot, moodKey string) string {
	m, ok := moods[moodKey]
	if !ok {
		return ""
	}
	switch slot {
	case "bg":
		return m.bg
	case "sheet", "barfg":
		return paper
	case "text":
		return ink
	case "link", "name", "barbg":
		return m.fg
	}
	return ""
}

var hexRE = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// ValidHex reports whether s is a #rrggbb colour, in either case.
func ValidHex(s string) bool {
	return hexRE.MatchString(strings.ToLower(s))
}
