package style_test

import (
	"slices"
	"testing"

	"github.com/mayloo89/retratar/internal/style"
)

func TestValidHex(t *testing.T) {
	for s, want := range map[string]bool{
		"#ff00cc": true, "#FF00CC": true, "#0a0B1c": true,
		"": false, "ff00cc": false, "#ff00c": false, "#ff00ccc": false,
		"#ff00cg": false, "#fff": false, " #ff00cc": false, "#ff00cc\n": false,
	} {
		if got := style.ValidHex(s); got != want {
			t.Errorf("ValidHex(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestSlots(t *testing.T) {
	var keys []string
	for _, s := range style.Slots() {
		keys = append(keys, s.Key)
		if s.Property == "" || s.Label == "" {
			t.Errorf("slot %q has an empty property or label", s.Key)
		}
	}
	if want := []string{"bg", "sheet", "text", "link", "name", "barbg", "barfg"}; !slices.Equal(keys, want) {
		t.Errorf("Slots() keys = %v, want %v", keys, want)
	}
}

func TestDefault_IsLowerCaseHex(t *testing.T) {
	for _, s := range style.Slots() {
		if got := style.Default(s.Key, "none"); !style.ValidHex(got) || got != lower(got) {
			t.Errorf("Default(%q, none) = %q, want lower-case #rrggbb", s.Key, got)
		}
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
