package user_test

import (
	"errors"
	"testing"
	"time"

	"github.com/mayloo89/retratar/internal/style"
	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
)

func TestStyleRoundTrip(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := login(t, svc, "ana@example.com")

	got, err := svc.GetStyle(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("GetStyle() with no row error = %v, want nil", err)
	}
	if !got.IsDefault() || !got.UpdatedAt.IsZero() {
		t.Fatalf("GetStyle() with no row = %+v, want the zero Style", got)
	}

	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	want := style.Style{
		Colors:     map[string]string{"bg": "#ff00cc", "text": "#39ff14", "barfg": "#000000"},
		HeadFont:   "impact",
		OwnMoodBox: true,
	}
	if err = svc.SetStyle(t.Context(), u.ID, want, now); err != nil {
		t.Fatalf("SetStyle() error = %v, want nil", err)
	}
	got, err = svc.GetStyle(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("GetStyle() error = %v, want nil", err)
	}
	if !got.Equal(want) {
		t.Errorf("GetStyle() = %+v, want %+v", got, want)
	}
	if !got.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, now)
	}

	// A second set replaces the first, slot for slot, and moves updated_at.
	later := now.Add(time.Hour)
	next := style.Style{Colors: map[string]string{"link": "#0000ee"}, BodyFont: "comic"}
	if err = svc.SetStyle(t.Context(), u.ID, next, later); err != nil {
		t.Fatalf("SetStyle() again error = %v, want nil", err)
	}
	got, _ = svc.GetStyle(t.Context(), u.ID)
	if !got.Equal(next) || !got.UpdatedAt.Equal(later) {
		t.Errorf("GetStyle() after replace = %+v, want %+v at %v", got, next, later)
	}
	if n := count(t, pool, "SELECT count(*) FROM page_styles"); n != 1 {
		t.Errorf("page_styles rows = %d, want 1", n)
	}
}

func TestSetStyle_NormalisesColourCase(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := login(t, svc, "ana@example.com")

	if err := svc.SetStyle(t.Context(), u.ID, style.Style{Colors: map[string]string{"bg": "#FF00CC"}}, time.Now()); err != nil {
		t.Fatalf("SetStyle() with an upper-case colour error = %v, want nil", err)
	}
	got, _ := svc.GetStyle(t.Context(), u.ID)
	if got.Colors["bg"] != "#ff00cc" {
		t.Errorf("stored colour = %q, want #ff00cc", got.Colors["bg"])
	}
}

func TestSetStyle_AllDefaultDeletesTheRow(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := login(t, svc, "ana@example.com")
	now := time.Now()

	// Nothing to store for an account with no row: still no row.
	if err := svc.SetStyle(t.Context(), u.ID, style.Style{}, now); err != nil {
		t.Fatalf("SetStyle(zero) error = %v, want nil", err)
	}
	if n := count(t, pool, "SELECT count(*) FROM page_styles"); n != 0 {
		t.Fatalf("page_styles rows = %d, want 0", n)
	}

	if err := svc.SetStyle(t.Context(), u.ID, style.Style{HeadFont: "comic"}, now); err != nil {
		t.Fatalf("SetStyle() error = %v, want nil", err)
	}
	if n := count(t, pool, "SELECT count(*) FROM page_styles"); n != 1 {
		t.Fatalf("page_styles rows = %d, want 1", n)
	}
	if err := svc.SetStyle(t.Context(), u.ID, style.Style{}, now); err != nil {
		t.Fatalf("SetStyle(zero) error = %v, want nil", err)
	}
	if n := count(t, pool, "SELECT count(*) FROM page_styles"); n != 0 {
		t.Errorf("page_styles rows after an all-default publish = %d, want 0", n)
	}
}

func TestSetStyle_RejectsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   style.Style
	}{
		{"colour without #", style.Style{Colors: map[string]string{"bg": "ff00cc"}}},
		{"short colour", style.Style{Colors: map[string]string{"bg": "#f0c"}}},
		{"colour name", style.Style{Colors: map[string]string{"bg": "red"}}},
		{"injection", style.Style{Colors: map[string]string{"bg": "#ff00cc;}"}}},
		{"empty colour", style.Style{Colors: map[string]string{"bg": ""}}},
		{"unknown slot", style.Style{Colors: map[string]string{"nope": "#ff00cc"}}},
		{"unknown head font", style.Style{HeadFont: "wingdings"}},
		{"unknown body font", style.Style{BodyFont: "wingdings"}},
		{"one bad among good", style.Style{Colors: map[string]string{"bg": "#ff00cc", "link": "nope"}, HeadFont: "comic"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pool := testdb.New(t)
			svc := user.NewService(pool)
			u := login(t, svc, "ana@example.com")

			if err := svc.SetStyle(t.Context(), u.ID, tt.in, time.Now()); !errors.Is(err, user.ErrInvalidStyle) {
				t.Fatalf("SetStyle() error = %v, want ErrInvalidStyle", err)
			}
			if n := count(t, pool, "SELECT count(*) FROM page_styles"); n != 0 {
				t.Errorf("page_styles rows = %d, want 0: a rejected style was stored", n)
			}
		})
	}
}

func TestSetStyle_RejectedKeepsTheStoredStyle(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := login(t, svc, "ana@example.com")
	kept := style.Style{Colors: map[string]string{"bg": "#ff00cc"}}
	if err := svc.SetStyle(t.Context(), u.ID, kept, time.Now()); err != nil {
		t.Fatalf("SetStyle() error = %v", err)
	}

	if err := svc.SetStyle(t.Context(), u.ID, style.Style{HeadFont: "wingdings"}, time.Now()); !errors.Is(err, user.ErrInvalidStyle) {
		t.Fatalf("SetStyle() error = %v, want ErrInvalidStyle", err)
	}
	got, _ := svc.GetStyle(t.Context(), u.ID)
	if !got.Equal(kept) {
		t.Errorf("GetStyle() = %+v, want the earlier style %+v", got, kept)
	}
}

func TestGetStyle_FontRemovedFromTheRegistryReadsAsDefault(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := login(t, svc, "ana@example.com")

	// A key the shape CHECK allows but the registry no longer has, as after a
	// font is dropped from the list.
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO page_styles (user_id, bg, font_head, font_body, updated_at)
		 VALUES ($1, '#ff00cc', 'gone-font', 'comic', now())`, u.ID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := svc.GetStyle(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("GetStyle() error = %v, want nil: a removed font is never an error", err)
	}
	if got.HeadFont != "" {
		t.Errorf("HeadFont = %q, want the default (empty) for a font no longer in the registry", got.HeadFont)
	}
	if got.BodyFont != "comic" || got.Colors["bg"] != "#ff00cc" {
		t.Errorf("GetStyle() = %+v, want the other choices kept", got)
	}
}

func TestPageStyles_ColumnsRejectBadShapes(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	svc := user.NewService(pool)
	u := login(t, svc, "ana@example.com")

	for name, q := range map[string]string{
		"upper-case colour": `INSERT INTO page_styles (user_id, bg, updated_at) VALUES ($1, '#FF00CC', now())`,
		"short colour":      `INSERT INTO page_styles (user_id, text_color, updated_at) VALUES ($1, '#fff', now())`,
		"font with space":   `INSERT INTO page_styles (user_id, font_body, updated_at) VALUES ($1, 'a b', now())`,
		"empty font":        `INSERT INTO page_styles (user_id, font_head, updated_at) VALUES ($1, '', now())`,
	} {
		if _, err := pool.Exec(t.Context(), q, u.ID); err == nil {
			t.Errorf("%s: insert succeeded, want a CHECK violation", name)
		}
	}
}
