-- +goose Up

-- An owner's current photo, and the allowlist that lets them have one.
--
-- photo_key names the file on disk (internal/photostore): 26 characters of
-- lowercase base32, random per upload. NULL means "no photo". The CHECK is the
-- same pattern as photostore.ValidKey, so a row can never name a path the store
-- would refuse. The unique index makes a key resolve to at most one owner,
-- which is what lets a page host check "is this photo mine" with one lookup.
--
-- photo_width and photo_height are the stored image's size, for <img width
-- height>. They are NULL exactly when photo_key is NULL; one CHECK ties the
-- three together so a half-written photo cannot exist.
--
-- photo_uploaded_at is the time of the last upload. Removing a photo does not
-- clear it, so remove-and-upload cannot get around the one-per-day limit.
--
-- can_upload_photos is the allowlist. The operator sets it with SQL; there is
-- no UI for it.
ALTER TABLE users
    ADD COLUMN photo_key text,
    ADD COLUMN photo_width integer,
    ADD COLUMN photo_height integer,
    ADD COLUMN photo_uploaded_at timestamptz,
    ADD COLUMN can_upload_photos boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT users_photo_key_format CHECK (photo_key IS NULL OR photo_key ~ '^[a-z2-7]{26}$'),
    ADD CONSTRAINT users_photo_columns_together CHECK (
        (photo_key IS NULL AND photo_width IS NULL AND photo_height IS NULL)
        OR (photo_key IS NOT NULL AND photo_width IS NOT NULL AND photo_height IS NOT NULL
            AND photo_width > 0 AND photo_height > 0)
    );

CREATE UNIQUE INDEX users_photo_key_key ON users (photo_key) WHERE photo_key IS NOT NULL;

-- +goose Down
DROP INDEX users_photo_key_key;
ALTER TABLE users
    DROP CONSTRAINT users_photo_columns_together,
    DROP CONSTRAINT users_photo_key_format,
    DROP COLUMN can_upload_photos,
    DROP COLUMN photo_uploaded_at,
    DROP COLUMN photo_height,
    DROP COLUMN photo_width,
    DROP COLUMN photo_key;
