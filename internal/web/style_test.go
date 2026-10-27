package web_test

import (
	"fmt"
	"image/color"
	"testing"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/ogcard"
	"github.com/mayloo89/retratar/internal/style"
)

func hexOf(c color.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func TestDefaultsMatchThemeCSS(t *testing.T) {
	css := readTheme(t)
	rules := cssRules(css)

	var root, body cssRule
	for _, r := range rules {
		switch r.selector {
		case ":root":
			root = r
		case "body":
			if declared(r, "--s-bg") != "" {
				body = r
			}
		}
	}
	consts := map[string]string{
		"--paper": declared(root, "--paper"),
		"--ink":   declared(root, "--ink"),
	}
	for name, v := range consts {
		if !style.ValidHex(v) {
			t.Fatalf(":root %s = %q, want a #rrggbb hex", name, v)
		}
	}

	palette, err := ogcard.ParsePalette([]byte(css))
	if err != nil {
		t.Fatalf("ParsePalette(theme.css) error = %v", err)
	}
	moods := append([]string{"none"}, keysOf(mood.AllKeys())...)
	if len(palette) != len(moods) {
		t.Errorf("theme.css has %d .mood-* rules, want %d (the moods plus none)", len(palette), len(moods))
	}

	// How theme.css gives each slot its default, by slot key.
	wantExpr := map[string]string{
		"bg": "var(--mood-bg)", "sheet": "var(--paper)", "text": "var(--ink)",
		"link": "var(--mood-fg)", "name": "var(--mood-fg)",
		"barbg": "var(--mood-fg)", "barfg": "var(--paper)",
	}
	slots := style.Slots()
	if len(slots) != len(wantExpr) {
		t.Fatalf("Slots() has %d entries, want %d", len(slots), len(wantExpr))
	}
	for _, s := range slots {
		if got := declared(body, s.Property); got != wantExpr[s.Key] {
			t.Fatalf("theme.css body %s = %q, want %q", s.Property, got, wantExpr[s.Key])
		}
	}

	for _, m := range moods {
		colors, ok := palette[m]
		if !ok {
			t.Errorf("theme.css has no .mood-%s rule", m)
			continue
		}
		for _, s := range slots {
			var want string
			switch wantExpr[s.Key] {
			case "var(--mood-bg)":
				want = hexOf(colors.Background)
			case "var(--mood-fg)":
				want = hexOf(colors.Foreground)
			case "var(--paper)":
				want = consts["--paper"]
			case "var(--ink)":
				want = consts["--ink"]
			}
			if got := style.Default(s.Key, m); got != want {
				t.Errorf("Default(%q, %q) = %q, want %q (theme.css)", s.Key, m, got, want)
			}
		}
	}

	if got := style.Default("bg", "nope"); got != "" {
		t.Errorf("Default(bg, nope) = %q, want \"\"", got)
	}
	if got := style.Default("nope", "feliz"); got != "" {
		t.Errorf("Default(nope, feliz) = %q, want \"\"", got)
	}
}

func keysOf(keys []mood.Key) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = string(k)
	}
	return out
}
