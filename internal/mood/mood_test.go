package mood_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/mood"
)

func TestAllKeysAreValidAndLabelled(t *testing.T) {
	t.Parallel()

	for _, k := range mood.AllKeys() {
		if !k.Valid() {
			t.Errorf("AllKeys() included %q, which is not Valid()", k)
		}
		if k.Label() == "" {
			t.Errorf("Label(%q) = \"\", want a display label", k)
		}
	}
}

func TestKeyValidRejectsUnknown(t *testing.T) {
	t.Parallel()

	if mood.Key("euforico").Valid() {
		t.Error("Valid() = true for a key outside the fixed vocabulary, want false")
	}
	if mood.Key("").Valid() {
		t.Error("Valid() = true for an empty key, want false")
	}
}

func TestNormaliseNote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{"empty is valid", "", "", nil},
		{"whitespace-only trims to empty", "   ", "", nil},
		{"trims surrounding space", "  todo bien  ", "todo bien", nil},
		{"60 runes is the boundary", strings.Repeat("é", 60), strings.Repeat("é", 60), nil},
		{"61 runes is rejected", strings.Repeat("é", 61), "", mood.ErrInvalidNote},
		{"embedded newline is rejected", "todo\nbien", "", mood.ErrInvalidNote},
		{"embedded tab is rejected", "todo\tbien", "", mood.ErrInvalidNote},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := mood.NormaliseNote(tt.raw)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("NormaliseNote(%q) error = %v, want nil", tt.raw, err)
				}
				if got != tt.want {
					t.Errorf("NormaliseNote(%q) = %q, want %q", tt.raw, got, tt.want)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NormaliseNote(%q) error = %v, want %v", tt.raw, err, tt.wantErr)
			}
		})
	}
}
