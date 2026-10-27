package style_test

import (
	"testing"

	"github.com/mayloo89/retratar/internal/style"
)

func TestFontByKey(t *testing.T) {
	f, ok := style.FontByKey("comic")
	if !ok || f.Label != "Comic" || f.Family() != "r-comic" {
		t.Errorf("FontByKey(comic) = %+v, %v", f, ok)
	}
	if _, ok := style.FontByKey(""); ok {
		t.Error("FontByKey(\"\") found a font, want none")
	}
}

func TestFonts_ReturnsACopy(t *testing.T) {
	a := style.Fonts()
	a[0].Key = "changed"
	if style.Fonts()[0].Key == "changed" {
		t.Error("Fonts() shares its backing array with the registry")
	}
}
