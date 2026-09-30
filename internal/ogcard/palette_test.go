package ogcard_test

import (
	"image/color"
	"os"
	"testing"

	"github.com/mayloo89/retratar/internal/mood"
	"github.com/mayloo89/retratar/internal/ogcard"
)

func TestParsePalette_ParsesBothHexForms(t *testing.T) {
	t.Parallel()

	css := []byte(`
.mood-feliz { --mood-bg: #fff4cc; --mood-fg: #5c4a00; }
.mood-none  { --mood-bg: #f4f4f4; --mood-fg: #222; }
`)

	p, err := ogcard.ParsePalette(css)
	if err != nil {
		t.Fatalf("ParsePalette() error = %v", err)
	}

	if got, want := p["feliz"].Background, (color.RGBA{R: 0xff, G: 0xf4, B: 0xcc, A: 0xff}); got != want {
		t.Errorf("feliz background = %v, want %v", got, want)
	}
	if got, want := p["none"].Foreground, (color.RGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff}); got != want {
		t.Errorf("none foreground (3-digit shorthand #222) = %v, want %v", got, want)
	}
}

func TestParsePalette_RejectsCSSWithNoMoodRules(t *testing.T) {
	t.Parallel()

	if _, err := ogcard.ParsePalette([]byte("body { color: red; }")); err == nil {
		t.Fatal("ParsePalette() error = nil, want an error for CSS with no .mood-<key> rules")
	}
}

// TestParsePalette_MatchesTheThemeEveryPageLoads is the test that keeps the
// OG card's colours from silently drifting away from theme.css: it parses
// the real stylesheet internal/web serves at /theme.css and checks that
// every mood.AllKeys() key, plus "none", comes back with a colour. A mood
// added to internal/mood without a matching rule in theme.css already
// breaks TestHandlePage_RendersMoodForAClaimedHandle-style rendering; this
// is the same guarantee for the card.
func TestParsePalette_MatchesTheThemeEveryPageLoads(t *testing.T) {
	t.Parallel()

	css, err := os.ReadFile("../web/static/theme.css")
	if err != nil {
		t.Fatalf("read theme.css: %v", err)
	}

	p, err := ogcard.ParsePalette(css)
	if err != nil {
		t.Fatalf("ParsePalette() error = %v", err)
	}

	for _, key := range mood.AllKeys() {
		if _, ok := p[string(key)]; !ok {
			t.Errorf("theme.css has no .mood-%s rule", key)
		}
	}
	if _, ok := p["none"]; !ok {
		t.Error("theme.css has no .mood-none rule")
	}
}
