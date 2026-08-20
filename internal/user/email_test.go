package user_test

import (
	"errors"
	"testing"

	"github.com/mayloo89/retratar/internal/user"
)

func TestNormaliseEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "already normal", in: "ana@example.com", want: "ana@example.com"},
		{name: "mixed case", in: "Ana@Example.COM", want: "ana@example.com"},
		{name: "padded", in: "  ana@example.com  ", want: "ana@example.com"},
		{name: "subdomain", in: "ana@mail.example.com.ar", want: "ana@mail.example.com.ar"},
		{name: "plus addressing", in: "ana+retratar@example.com", want: "ana+retratar@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := user.NormaliseEmail(tt.in)
			if err != nil {
				t.Fatalf("NormaliseEmail(%q) error = %v, want nil", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormaliseEmail(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormaliseEmailRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "whitespace", in: "   "},
		{name: "no at sign", in: "ana"},
		{name: "no domain", in: "ana@"},
		{name: "no local part", in: "@example.com"},
		// Valid to a mail parser and undeliverable in practice: a magic link
		// has to leave the building.
		{name: "no dot in the domain", in: "ana@localhost"},
		{name: "trailing dot", in: "ana@example.com."},
		{name: "two at signs", in: "ana@example@com"},
		// A display name would let one address arrive in two spellings, and
		// would put text somebody else chose into anything that echoes it back.
		{name: "display name", in: "Ana <ana@example.com>"},
		{name: "longer than SMTP carries", in: longAddress()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := user.NormaliseEmail(tt.in)
			if !errors.Is(err, user.ErrInvalidEmail) {
				t.Fatalf("NormaliseEmail(%q) = %q, error = %v, want ErrInvalidEmail", tt.in, got, err)
			}
		})
	}
}

// longAddress builds a syntactically fine address past the 254-byte limit SMTP
// is required to carry.
func longAddress() string {
	local := make([]byte, 250)
	for i := range local {
		local[i] = 'a'
	}
	return string(local) + "@example.com"
}
