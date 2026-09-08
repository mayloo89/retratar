package user_test

import (
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/user"
)

func TestValidHandle(t *testing.T) {
	tests := []struct {
		handle string
		want   bool
		why    string
	}{
		{"sebas", true, ""},
		{"ana-lucia", true, ""},
		{"x", true, ""},
		{"user2000", true, ""},
		{"", false, "empty"},
		{strings.Repeat("a", 31), false, "too long"},
		{"Sebas", false, "uppercase is not a canonical DNS label"},
		{"-sebas", false, "leading hyphen is not a valid DNS label"},
		{"sebas-", false, "trailing hyphen is not a valid DNS label"},
		{"se--bas", false, "double hyphen is how punycode is encoded"},
		{"xn--fiqs8s", false, "punycode prefix"},
		{"se_bas", false, "underscore is not a valid DNS label"},
		{"se.bas", false, "a dot would create another level"},
		{"sebás", false, "non-ASCII enables homograph impersonation"},
		{"аna", false, "Cyrillic а impersonates Latin a"},
		{"admin", false, "reserved"},
		{"www", false, "reserved"},
		{"api", false, "reserved"},
	}

	for _, tt := range tests {
		name := tt.handle
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			if got := user.ValidHandle(tt.handle); got != tt.want {
				t.Errorf("ValidHandle(%q) = %v, want %v (%s)", tt.handle, got, tt.want, tt.why)
			}
		})
	}
}

func TestValidHandleLengthBoundary(t *testing.T) {
	if !user.ValidHandle(strings.Repeat("a", 30)) {
		t.Error("30-character handle rejected, want accepted")
	}
	if user.ValidHandle(strings.Repeat("a", 31)) {
		t.Error("31-character handle accepted, want rejected")
	}
}
