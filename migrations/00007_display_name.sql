-- +goose Up

-- An optional name shown on the owner's page in place of the handle. NULL means
-- "none": the page falls back to the handle, so there is no empty-string state
-- to tell apart from absent. The CHECK mirrors the application's 40-rune limit
-- (user.NormaliseDisplayName); char_length counts characters, not bytes, so an
-- accented letter costs the same as a plain one.
ALTER TABLE users
    ADD COLUMN display_name text
    CONSTRAINT users_display_name_length CHECK (display_name IS NULL OR char_length(display_name) BETWEEN 1 AND 40);

-- +goose Down
ALTER TABLE users DROP COLUMN display_name;
