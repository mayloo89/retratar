package ogcard_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/ogcard"
)

func testPalette(t *testing.T) ogcard.Palette {
	t.Helper()
	css := []byte(`
.mood-feliz { --mood-bg: #fff4cc; --mood-fg: #5c4a00; }
.mood-none  { --mood-bg: #f4f4f4; --mood-fg: #222222; }
`)
	p, err := ogcard.ParsePalette(css)
	if err != nil {
		t.Fatalf("ParsePalette() error = %v", err)
	}
	return p
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return img
}

func TestRender_DimensionsAndBackgroundColour(t *testing.T) {
	t.Parallel()

	palette := testPalette(t)
	card := ogcard.Card{Handle: "sebas", MoodClass: "feliz", MoodLabel: "feliz", MoodNote: "un buen día"}

	out, err := ogcard.Render(card, palette)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	img := decode(t, out)
	if b := img.Bounds(); b.Dx() != ogcard.Width || b.Dy() != ogcard.Height {
		t.Fatalf("dimensions = %dx%d, want %dx%d", b.Dx(), b.Dy(), ogcard.Width, ogcard.Height)
	}

	// A corner is far from any glyph regardless of layout, so it is safe to
	// assert it is exactly the mood's background colour.
	got := img.At(2, 2)
	want := color.RGBA{R: 0xff, G: 0xf4, B: 0xcc, A: 0xff}
	if got != want {
		t.Errorf("corner pixel = %v, want %v", got, want)
	}
}

func TestRender_IsDeterministic(t *testing.T) {
	t.Parallel()

	palette := testPalette(t)
	card := ogcard.Card{Handle: "sebas", MoodClass: "feliz", MoodLabel: "feliz", MoodNote: "un buen día"}

	a, err := ogcard.Render(card, palette)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	b, err := ogcard.Render(card, palette)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("Render() produced different bytes for the same Card twice")
	}
}

func TestRender_UnknownMoodClassFallsBackToNone(t *testing.T) {
	t.Parallel()

	palette := testPalette(t)
	// This should never happen — MoodClass comes from mood.Key.Valid() data
	// — but Render must not panic if it does.
	card := ogcard.Card{Handle: "sebas", MoodClass: "not-a-real-mood"}

	out, err := ogcard.Render(card, palette)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	img := decode(t, out)
	got := img.At(2, 2)
	want := color.RGBA{R: 0xf4, G: 0xf4, B: 0xf4, A: 0xff}
	if got != want {
		t.Errorf("corner pixel = %v, want the none-fallback background %v", got, want)
	}
}

func TestRender_EmptyStateWhenNoMoodSet(t *testing.T) {
	t.Parallel()

	palette := testPalette(t)
	card := ogcard.Card{Handle: "sebas", MoodClass: "none"}

	if _, err := ogcard.Render(card, palette); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
}

// TestRender_AdversarialNotesNeverPanicOrHang is the layout-safety net the
// task calls for: /og.png is unauthenticated, so whatever text layout does
// with an unusual note must not panic or loop, even though
// mood.NormaliseNote already bounds length and rejects control characters
// before a note ever reaches here.
func TestRender_AdversarialNotesNeverPanicOrHang(t *testing.T) {
	t.Parallel()

	palette := testPalette(t)

	notes := map[string]string{
		"empty":               "",
		"max length ascii":    strings.Repeat("a", 60),
		"single long word":    strings.Repeat("x", 60),
		"emoji":               strings.Repeat("😀🎉🚀", 20),
		"wide CJK":            strings.Repeat("一", 60),
		"combining marks":     strings.Repeat("é́́", 20), // e + repeated combining acute
		"RTL":                 strings.Repeat("مرحبا بالعالم ", 5),
		"mixed width and gap": "a 一 😀 b 二 🎉 " + strings.Repeat("c", 40),
		"zero width joiner":   strings.Repeat("👨‍👩‍👧", 10),
		"only spaces":         strings.Repeat(" ", 60),
	}

	for name, note := range notes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			card := ogcard.Card{Handle: "sebas", MoodClass: "feliz", MoodLabel: "feliz", MoodNote: note}
			out, err := ogcard.Render(card, palette)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			img := decode(t, out)
			if b := img.Bounds(); b.Dx() != ogcard.Width || b.Dy() != ogcard.Height {
				t.Errorf("dimensions = %dx%d, want %dx%d", b.Dx(), b.Dy(), ogcard.Width, ogcard.Height)
			}
		})
	}
}

// TestRender_LongHandleNeverPanicsOrOverflows covers the widest handle
// user.ValidHandle allows (30 characters), including one made entirely of a
// glyph wide enough that the fixed-size handle text would otherwise overrun
// the card.
func TestRender_LongHandleNeverPanicsOrOverflows(t *testing.T) {
	t.Parallel()

	palette := testPalette(t)
	handles := []string{
		strings.Repeat("w", 30),
		strings.Repeat("一", 30),
		"",
	}

	for _, h := range handles {
		card := ogcard.Card{Handle: h, MoodClass: "feliz", MoodLabel: "feliz", MoodNote: "nota"}
		out, err := ogcard.Render(card, palette)
		if err != nil {
			t.Fatalf("Render(handle=%q) error = %v", h, err)
		}
		img := decode(t, out)
		if b := img.Bounds(); b.Dx() != ogcard.Width || b.Dy() != ogcard.Height {
			t.Errorf("Render(handle=%q) dimensions = %dx%d, want %dx%d", h, b.Dx(), b.Dy(), ogcard.Width, ogcard.Height)
		}
	}
}

// FuzzRender backstops the table test above with real fuzzing over the note
// text — `go test -fuzz=FuzzRender ./internal/ogcard` seeds new corpus
// entries under testdata/fuzz on any failure. Handle and mood label are held
// to realistic values so a finding is about note text specifically.
func FuzzRender(f *testing.F) {
	for _, seed := range []string{
		"", "hola", strings.Repeat("a", 60), "😀🎉🚀", "一二三", "مرحبا", "\u200d\u0301", // last: bare ZWJ + combining mark, no base rune
	} {
		f.Add(seed)
	}

	css := []byte(`.mood-feliz { --mood-bg: #fff4cc; --mood-fg: #5c4a00; }
.mood-none { --mood-bg: #f4f4f4; --mood-fg: #222222; }`)
	palette, err := ogcard.ParsePalette(css)
	if err != nil {
		f.Fatalf("ParsePalette() error = %v", err)
	}

	f.Fuzz(func(t *testing.T, note string) {
		card := ogcard.Card{Handle: "sebas", MoodClass: "feliz", MoodLabel: "feliz", MoodNote: note}
		if _, err := ogcard.Render(card, palette); err != nil {
			t.Fatalf("Render(note=%q) error = %v", note, err)
		}
	})
}
