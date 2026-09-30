// Package ogcard renders the Open Graph preview image for a public page:
// handle, mood label and note, on the mood's colour, as a fixed-size PNG.
//
// It knows nothing about HTTP, the database, or [mood.Key] — it takes a
// [Card] and a [Palette] and returns bytes. That keeps it unit-testable
// without a server, and keeps the render cost of /og.png (see
// internal/web/og.go) independent of anything client-controlled.
package ogcard

import (
	"fmt"
	"image/color"
	"regexp"
)

// Colors is the background/foreground pair a mood tints a page with.
type Colors struct {
	Background color.RGBA
	Foreground color.RGBA
}

// Palette maps a mood key (or "none", for the no-mood-yet state) to the
// colours a page uses for it.
type Palette map[string]Colors

// hexColorPattern matches a CSS hex color in either 6-digit (#rrggbb) or
// 3-digit shorthand (#rgb) form — theme.css uses both (.mood-none's #222 is
// shorthand).
const hexColorPattern = `#([0-9a-fA-F]{6}|[0-9a-fA-F]{3})`

// moodRuleRE matches one ".mood-<key> { --mood-bg: ...; --mood-fg: ...; }"
// rule in theme.css. It is deliberately narrow — anchored to the two custom
// properties the CSS actually declares — so a rule it cannot parse is a real
// error, not a silently skipped mood.
var moodRuleRE = regexp.MustCompile(
	`\.mood-([a-z]+)\s*\{[^}]*--mood-bg:\s*` + hexColorPattern + `[^}]*--mood-fg:\s*` + hexColorPattern + `[^}]*\}`)

// ParsePalette extracts the OG card's colours from theme.css — the same
// stylesheet every public page loads. Parsing the embedded CSS, rather than
// keeping a second Go map of the same colours, is what keeps the two from
// silently drifting apart: a new or recoloured mood only ever needs to
// change in theme.css.
func ParsePalette(css []byte) (Palette, error) {
	matches := moodRuleRE.FindAllSubmatch(css, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("ogcard: no .mood-<key> rules found in theme.css")
	}

	p := make(Palette, len(matches))
	for _, m := range matches {
		key := string(m[1])
		bg, err := parseHexColor(m[2])
		if err != nil {
			return nil, fmt.Errorf("ogcard: mood %q background: %w", key, err)
		}
		fg, err := parseHexColor(m[3])
		if err != nil {
			return nil, fmt.Errorf("ogcard: mood %q foreground: %w", key, err)
		}
		p[key] = Colors{Background: bg, Foreground: fg}
	}
	return p, nil
}

// parseHexColor converts a 3- or 6-hex-digit match (already validated by
// moodRuleRE) into an opaque color.RGBA. A 3-digit shorthand doubles each
// nibble, per the CSS spec: #222 is #222222, not #202020.
func parseHexColor(hex []byte) (color.RGBA, error) {
	full := hex
	if len(hex) == 3 {
		full = []byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]}
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(string(full), "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{}, err
	}
	return color.RGBA{R: r, G: g, B: b, A: 0xff}, nil
}
