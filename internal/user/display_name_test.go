package user_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
)

func TestNormaliseDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{name: "plain", in: "Seba", want: "Seba"},
		{name: "trims whitespace", in: "  Seba \t\n", want: "Seba"},
		{name: "empty clears", in: "", want: ""},
		{name: "whitespace only clears", in: " \t ", want: ""},
		{name: "inner spaces and case", in: "María del Mar", want: "María del Mar"},
		{name: "40 runes with accents", in: strings.Repeat("é", 40), want: strings.Repeat("é", 40)},
		{name: "40 runes of emoji", in: strings.Repeat("🙂", 40), want: strings.Repeat("🙂", 40)},
		{name: "41 runes", in: strings.Repeat("a", 41), wantErr: user.ErrInvalidDisplayName},
		{name: "41 accented runes", in: strings.Repeat("é", 41), wantErr: user.ErrInvalidDisplayName},
		{name: "line break", in: "Se\nba", wantErr: user.ErrInvalidDisplayName},
		{name: "tab", in: "Se\tba", wantErr: user.ErrInvalidDisplayName},
		{name: "carriage return", in: "Se\rba", wantErr: user.ErrInvalidDisplayName},
		{name: "invalid utf-8", in: "Se\xffba", wantErr: user.ErrInvalidDisplayName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := user.NormaliseDisplayName(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NormaliseDisplayName(%q) error = %v, want %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("NormaliseDisplayName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSetDisplayNameRoundTrip(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := login(t, svc, "ana@example.com")

	got, err := svc.SetDisplayName(t.Context(), u.ID, "  Ana María ")
	if err != nil {
		t.Fatalf("SetDisplayName() error = %v, want nil", err)
	}
	if got.DisplayName != "Ana María" {
		t.Errorf("SetDisplayName() DisplayName = %q, want %q", got.DisplayName, "Ana María")
	}
	read, err := svc.GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v, want nil", err)
	}
	if read.DisplayName != "Ana María" {
		t.Errorf("GetByID() DisplayName = %q, want %q", read.DisplayName, "Ana María")
	}

	if _, err = svc.SetDisplayName(t.Context(), u.ID, ""); err != nil {
		t.Fatalf("SetDisplayName(\"\") error = %v, want nil", err)
	}
	read, err = svc.GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v, want nil", err)
	}
	if read.DisplayName != "" {
		t.Errorf("DisplayName after clearing = %q, want empty", read.DisplayName)
	}
	if n := count(t, pool, "SELECT count(*) FROM users WHERE display_name IS NULL"); n != 1 {
		t.Errorf("users with NULL display_name = %d, want 1: clearing must store NULL, not an empty string", n)
	}
}

func TestSetDisplayNameRejectsInvalid(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := login(t, svc, "ana@example.com")

	if _, err := svc.SetDisplayName(t.Context(), u.ID, strings.Repeat("a", 41)); !errors.Is(err, user.ErrInvalidDisplayName) {
		t.Fatalf("SetDisplayName(41 runes) error = %v, want ErrInvalidDisplayName", err)
	}
	if n := count(t, pool, "SELECT count(*) FROM users WHERE display_name IS NULL"); n != 1 {
		t.Errorf("a rejected name was stored")
	}
}

func TestShownName(t *testing.T) {
	t.Parallel()

	if got := (user.User{Handle: "sebas", DisplayName: "Seba"}).ShownName(); got != "Seba" {
		t.Errorf("ShownName() with a display name = %q, want %q", got, "Seba")
	}
	if got := (user.User{Handle: "sebas"}).ShownName(); got != "sebas" {
		t.Errorf("ShownName() without a display name = %q, want %q", got, "sebas")
	}
}
