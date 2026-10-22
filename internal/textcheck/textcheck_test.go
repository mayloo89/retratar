package textcheck

import "testing"

func TestLineAndBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        string
		wantLine  bool
		wantBlock bool
	}{
		{"plain", "hola chau", true, true},
		{"accents", "canción ñandú", true, true},
		{"emoji", "hola 🎉", true, true},
		{"zwj family emoji", "👨‍👩‍👧", true, true},
		{"empty", "", true, true},
		{"newline", "hola\nchau", false, true},
		{"tab", "hola\tchau", false, false},
		{"carriage return", "hola\rchau", false, false},
		{"nul", "hola\x00chau", false, false},
		{"invalid utf-8", "\xff", false, false},
		{"invalid utf-8 after valid text", "hola\xc3", false, false},
		{"U+202A", "a\u202ab", false, false},
		{"U+202E", "a\u202eb", false, false},
		{"U+2066", "a\u2066b", false, false},
		{"U+2069", "a\u2069b", false, false},
		{"U+200E", "a\u200eb", false, false},
		{"U+200F", "a\u200fb", false, false},
		{"U+2028", "a\u2028b", false, false},
		{"U+2029", "a\u2029b", false, false},
		{"just below U+202A", "a\u2027b", true, true},
		{"just above U+202E", "a b", true, true},
		{"just below U+2066", "a⁥b", true, true},
		{"just above U+2069", "a\u206ab", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Line(tt.in); got != tt.wantLine {
				t.Errorf("Line(%q) = %v, want %v", tt.in, got, tt.wantLine)
			}
			if got := Block(tt.in); got != tt.wantBlock {
				t.Errorf("Block(%q) = %v, want %v", tt.in, got, tt.wantBlock)
			}
		})
	}
}
