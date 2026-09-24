package ogcard

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Width and Height are the card's fixed output dimensions — 1200x630 is the
// size every OG consumer (WhatsApp, Telegram, Discord, iMessage, Twitter,
// Slack) expects for the wide "summary_large_image" layout. They do not vary
// with input: nothing about the request can change how much work Render
// does.
const (
	Width  = 1200
	Height = 630
)

const (
	padding = 80

	handleSize = 64
	// handleMinSize is the floor fitSize shrinks the handle to. A handle at
	// user.ValidHandle's 30-character limit, all wide glyphs, still fits
	// comfortably at this size — see internal/ogcard/render_test.go.
	handleMinSize = 28
	handleStep    = 4

	labelSize = 40
	noteSize  = 34
	lineGap   = 1.35 // multiplied by the face size to get line height

	// maxNoteLines bounds how many lines the note wraps to. It, not the
	// note's rune count, is what actually bounds render cost: a 60-rune
	// string of wide CJK glyphs would otherwise wrap to far more lines than
	// a 60-rune string of Latin ones. A note that still doesn't fit past
	// this many lines is truncated with an ellipsis on the last one.
	maxNoteLines = 4

	dpi = 72
)

// EmptyStateText is shown in place of a mood label and note when the
// account has never set one. It is exported so internal/web can reuse the
// exact same copy for the page's own empty state and its og:description —
// three independent copies of this sentence is exactly the kind of drift
// the palette's single-source-of-truth parsing (see ParsePalette) exists to
// avoid elsewhere; this is the same guarantee for the text.
const EmptyStateText = "Todavía no eligió un estado de ánimo."

// noneClass is the palette key for the no-mood-yet state — see
// internal/web/static/theme.css's .mood-none rule and handlePage's
// pageData.MoodClass default.
const noneClass = "none"

// regularFont and boldFont are parsed once at package init from the Go font
// family bundled with golang.org/x/image (see docs/decisions/0006). Parsing
// is the expensive, input-independent part; [font.Face] values built from
// them are cheap to create per request and, unlike the parsed *opentype.Font,
// are not safe to share across goroutines.
var (
	regularFont *opentype.Font
	boldFont    *opentype.Font
)

func init() {
	var err error
	regularFont, err = opentype.Parse(goregular.TTF)
	if err != nil {
		panic(fmt.Sprintf("ogcard: parse embedded regular font: %v", err))
	}
	boldFont, err = opentype.Parse(gobold.TTF)
	if err != nil {
		panic(fmt.Sprintf("ogcard: parse embedded bold font: %v", err))
	}
}

// Card is the content one OG image shows. It is deliberately its own type
// rather than a reuse of mood.Mood, so this package has no dependency on the
// mood package and no way to reach into the database.
type Card struct {
	Handle string

	// MoodClass keys the Palette lookup. It is "none" for the no-mood-yet
	// state, or one of mood.AllKeys()'s string values otherwise — the
	// caller is responsible for that, the same way pageData.MoodClass is in
	// internal/web/page.go.
	MoodClass string
	MoodLabel string
	MoodNote  string
}

// Render draws c onto its mood's background colour and returns an encoded
// PNG. It never fails on the content of c — Card fields come from data this
// program already validated when it was written (see mood.NormaliseNote) —
// only on a Palette that lacks both c.MoodClass and the "none" fallback,
// which would mean ParsePalette was given a theme.css missing a mood this
// program still knows about.
func Render(c Card, palette Palette) ([]byte, error) {
	colors, ok := palette[c.MoodClass]
	if !ok {
		colors, ok = palette[noneClass]
		if !ok {
			return nil, fmt.Errorf("ogcard: palette has no %q and no fallback %q", c.MoodClass, noneClass)
		}
	}

	img := image.NewRGBA(image.Rect(0, 0, Width, Height))
	draw.Draw(img, img.Bounds(), image.NewUniform(colors.Background), image.Point{}, draw.Src)

	if err := drawCard(img, c, colors); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("ogcard: encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// drawCard lays out the handle, then either the empty-state line or the
// mood label and wrapped note, top to bottom.
func drawCard(img *image.RGBA, c Card, colors Colors) error {
	maxWidth := fixed.I(Width - 2*padding)

	// The handle is one line, never wrapped — unlike the note it is an
	// identity, not prose, so wrapping it would look broken. handleMaxLen
	// (30, see user.ValidHandle) times handleSize bold could still overflow
	// maxWidth for a handle made entirely of wide characters, so it shrinks
	// to fit instead of wrapping or being clipped.
	handleFitSize, err := fitSize(boldFont, c.Handle, handleSize, handleMinSize, maxWidth)
	if err != nil {
		return err
	}
	handleFace, err := newFace(boldFont, handleFitSize)
	if err != nil {
		return err
	}
	defer func() { _ = handleFace.Close() }()

	labelFace, err := newFace(boldFont, labelSize)
	if err != nil {
		return err
	}
	defer func() { _ = labelFace.Close() }()

	noteFace, err := newFace(regularFont, noteSize)
	if err != nil {
		return err
	}
	defer func() { _ = noteFace.Close() }()

	src := image.NewUniform(colors.Foreground)

	y := padding + handleSize
	drawLine(img, handleFace, src, c.Handle, padding, y)
	y += lineHeight(labelSize)

	if c.MoodLabel == "" && c.MoodNote == "" {
		y += labelSize
		for _, line := range wrapLines(noteFace, EmptyStateText, maxWidth, maxNoteLines) {
			drawLine(img, noteFace, src, line, padding, y)
			y += lineHeight(noteSize)
		}
		return nil
	}

	y += labelSize
	drawLine(img, labelFace, src, c.MoodLabel, padding, y)
	y += lineHeight(noteSize)

	for _, line := range wrapLines(noteFace, c.MoodNote, maxWidth, maxNoteLines) {
		y += lineHeight(noteSize)
		drawLine(img, noteFace, src, line, padding, y)
	}
	return nil
}

// lineHeight converts a face size in points to a pixel line height. It takes
// size as a parameter, rather than being an inline size*lineGap expression,
// so the multiplication happens at runtime: size*lineGap is not an integer
// for these sizes, and Go refuses to truncate a non-integer constant
// expression at compile time.
func lineHeight(size float64) int {
	return int(size * lineGap)
}

// fitSize returns the largest size, from maxSize down to handleMinSize in
// handleStep increments, at which text measures no wider than maxWidth. The
// loop runs a fixed number of times — (maxSize-handleMinSize)/handleStep+1,
// nine for the handle's current bounds — regardless of text, so a
// pathological handle costs no more than a short one.
func fitSize(f *opentype.Font, text string, maxSize, minSize float64, maxWidth fixed.Int26_6) (float64, error) {
	for size := maxSize; size > minSize; size -= handleStep {
		face, err := newFace(f, size)
		if err != nil {
			return 0, err
		}
		width := font.MeasureString(face, text)
		_ = face.Close()
		if width <= maxWidth {
			return size, nil
		}
	}
	return minSize, nil
}

// newFace builds a font.Face at size points, 72 DPI, with full hinting —
// the card is a fixed-size raster, so there is no reason to skip it.
func newFace(f *opentype.Font, size float64) (font.Face, error) {
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     dpi,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("ogcard: build font face: %w", err)
	}
	return face, nil
}

// drawLine draws s with its baseline at (x, y) in pixels.
func drawLine(img *image.RGBA, face font.Face, src image.Image, s string, x, y int) {
	d := &font.Drawer{
		Dst:  img,
		Src:  src,
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	d.DrawString(s)
}

// wrapLines breaks s into at most maxLines lines that each fit maxWidth,
// breaking by rune rather than by word. A rune-by-rune break, not a
// word-boundary one, is what keeps this bounded and panic-free for input
// with no word boundaries at all — a 60-rune string of wide CJK glyphs, or
// one with no spaces — without a separate code path for that case. The
// trade-off is a note can wrap mid-word; for a one-line away-message on an
// OG card that is a cosmetic cost, not a correctness one.
//
// s is assumed to already be within mood.NormaliseNote's bound (60 runes,
// no control characters); wrapLines still runs in O(runes) either way, so
// nothing here loops or allocates unboundedly on longer input.
func wrapLines(face font.Face, s string, maxWidth fixed.Int26_6, maxLines int) []string {
	if s == "" {
		return nil
	}

	runes := []rune(s)
	var lines []string
	d := &font.Drawer{Face: face}

	for len(runes) > 0 && len(lines) < maxLines {
		last := len(lines) == maxLines-1

		end := len(runes)
		for end > 0 && d.MeasureString(string(runes[:end])) > maxWidth {
			end--
		}
		if end == 0 {
			// Not even one rune fits (a pathologically tiny maxWidth). Take
			// one anyway so the loop always makes progress.
			end = 1
		}

		if last && end < len(runes) {
			lines = append(lines, truncateWithEllipsis(d, runes, maxWidth))
			return lines
		}

		lines = append(lines, string(runes[:end]))
		runes = runes[end:]
	}
	return lines
}

// truncateWithEllipsis returns as many leading runes as fit in maxWidth
// alongside a trailing "…".
func truncateWithEllipsis(d *font.Drawer, runes []rune, maxWidth fixed.Int26_6) string {
	const ellipsis = "…"
	end := len(runes)
	for end > 0 && d.MeasureString(string(runes[:end])+ellipsis) > maxWidth {
		end--
	}
	return string(runes[:end]) + ellipsis
}
