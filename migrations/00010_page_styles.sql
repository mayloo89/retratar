-- +goose Up

-- An owner's choices for their page's colours and fonts. One row per owner,
-- and only when something differs from the default: no row means "all
-- default", and publishing an all-default style deletes the row, so "no
-- style" has one representation.
--
-- Every column but own_mood_box and updated_at is NULL for "Por defecto": the
-- page keeps following the mood for that slot. A colour is #rrggbb in lower
-- case, the same pattern as style.ValidHex after normalising; a font is a key
-- of the registry in internal/style. The CHECKs only bound the shape. That a
-- font key is in the registry is checked in Go, and a key that leaves the
-- registry later reads as NULL rather than as an error.
--
-- The colour columns are the slots of internal/style (bg, sheet, text, link,
-- name, barbg, barfg) under their own names, except text_color, because text
-- is a type name. internal/user maps columns to slot keys in one place.
--
-- own_mood_box is true when the "cómo estoy" box takes the owner's colours
-- instead of keeping the mood's. updated_at is the application's clock; the
-- page uses it as the stylesheet's cache-busting version.
--
-- Deleting the account deletes the row.
CREATE TABLE page_styles (
    user_id      uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    bg           text CHECK (bg IS NULL OR bg ~ '^#[0-9a-f]{6}$'),
    sheet        text CHECK (sheet IS NULL OR sheet ~ '^#[0-9a-f]{6}$'),
    text_color   text CHECK (text_color IS NULL OR text_color ~ '^#[0-9a-f]{6}$'),
    link         text CHECK (link IS NULL OR link ~ '^#[0-9a-f]{6}$'),
    name         text CHECK (name IS NULL OR name ~ '^#[0-9a-f]{6}$'),
    bar_bg       text CHECK (bar_bg IS NULL OR bar_bg ~ '^#[0-9a-f]{6}$'),
    bar_fg       text CHECK (bar_fg IS NULL OR bar_fg ~ '^#[0-9a-f]{6}$'),
    font_head    text CHECK (font_head IS NULL OR font_head ~ '^[a-z0-9-]{1,32}$'),
    font_body    text CHECK (font_body IS NULL OR font_body ~ '^[a-z0-9-]{1,32}$'),
    own_mood_box boolean NOT NULL DEFAULT false,
    updated_at   timestamptz NOT NULL
);

-- +goose Down
DROP TABLE page_styles;
