package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mayloo89/retratar/internal/store"
	"github.com/mayloo89/retratar/internal/style"
)

// colorColumn returns the column of row that holds the slot with the given
// key, or nil for a key that is not a slot. It is the one place slot keys meet
// column names; GetStyle and SetStyle both go through it.
func colorColumn(row *store.PageStyle, key string) **string {
	switch key {
	case "bg":
		return &row.Bg
	case "sheet":
		return &row.Sheet
	case "text":
		return &row.TextColor
	case "link":
		return &row.Link
	case "name":
		return &row.Name
	case "barbg":
		return &row.BarBg
	case "barfg":
		return &row.BarFg
	}
	return nil
}

// GetStyle returns the account's page style. An account with no row has the
// zero Style, which is the same as every slot at its default.
//
// A font key that is no longer in the registry reads as the default, never as
// an error: the registry may lose a font after an owner picked it.
func (s *Service) GetStyle(ctx context.Context, id uuid.UUID) (style.Style, error) {
	row, err := s.queries.GetPageStyle(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return style.Style{}, nil
		}
		return style.Style{}, fmt.Errorf("get page style: %w", err)
	}

	st := style.Style{OwnMoodBox: row.OwnMoodBox, UpdatedAt: row.UpdatedAt.Time}
	for _, slot := range style.Slots() {
		col := colorColumn(&row, slot.Key)
		if col == nil || *col == nil {
			continue
		}
		// The CHECK keeps the column well-formed; ParseHex is what makes the
		// value one this program formatted.
		hex, ok := style.ParseHex(**col)
		if !ok {
			continue
		}
		if st.Colors == nil {
			st.Colors = map[string]string{}
		}
		st.Colors[slot.Key] = hex
	}
	st.HeadFont = knownFont(row.FontHead)
	st.BodyFont = knownFont(row.FontBody)
	return st, nil
}

// knownFont returns the stored key if the registry still has it, else "".
func knownFont(stored *string) string {
	if stored == nil {
		return ""
	}
	if _, ok := style.FontByKey(*stored); !ok {
		return ""
	}
	return *stored
}

// SetStyle stores the account's page style, replacing the previous one. It
// validates again whatever the caller already checked: every colour key must
// be a slot and every colour #rrggbb (stored in lower case), and every font
// must be in the registry. Anything else returns [ErrInvalidStyle] and stores
// nothing.
//
// A style that changes nothing deletes the row instead, so "no style" has one
// representation. now is the application's clock and becomes updated_at,
// which the page uses to bust its stylesheet's cache.
func (s *Service) SetStyle(ctx context.Context, id uuid.UUID, st style.Style, now time.Time) error {
	row := store.PageStyle{
		UserID:     id,
		OwnMoodBox: st.OwnMoodBox,
		UpdatedAt:  pgtype.Timestamptz{Time: now, Valid: true},
	}
	for key, hex := range st.Colors {
		col := colorColumn(&row, key)
		parsed, ok := style.ParseHex(hex)
		if col == nil || !ok {
			return ErrInvalidStyle
		}
		*col = &parsed
	}
	for _, f := range []struct {
		key string
		dst **string
	}{{st.HeadFont, &row.FontHead}, {st.BodyFont, &row.FontBody}} {
		if f.key == "" {
			continue
		}
		if _, ok := style.FontByKey(f.key); !ok {
			return ErrInvalidStyle
		}
		key := f.key
		*f.dst = &key
	}

	if st.IsDefault() {
		if err := s.queries.DeletePageStyle(ctx, id); err != nil {
			return fmt.Errorf("delete page style: %w", err)
		}
		return nil
	}
	// The params have the row's fields in the row's order, so this is a
	// conversion, not a copy that could drift.
	if err := s.queries.UpsertPageStyle(ctx, store.UpsertPageStyleParams(row)); err != nil {
		return fmt.Errorf("upsert page style: %w", err)
	}
	return nil
}
