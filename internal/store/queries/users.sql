-- name: UpsertUserByEmail :one
--
-- Returns the account for an email, creating it on first use. Login is a signup
-- when the account does not exist yet; there is no separate registration step
-- to keep in sync.
--
-- The DO UPDATE writes the value back unchanged on purpose. DO NOTHING would be
-- the honest expression of the intent, but it returns no row on conflict, and
-- this query must return the existing account.
INSERT INTO users (email)
VALUES (@email)
ON CONFLICT (lower(email)) DO UPDATE SET email = users.email
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id;

-- name: ClaimHandle :one
--
-- The WHERE guards against re-claiming: once handle is set, this matches no
-- row and the caller sees pgx.ErrNoRows, the same shape ConsumeLoginToken
-- already produces for "this cannot proceed." A second, different user
-- claiming the same handle instead hits users_handle_key and fails as a
-- unique violation, which the caller distinguishes from "already set."
UPDATE users
SET handle = @handle, state = 'active'
WHERE id = @id AND handle IS NULL
RETURNING *;

-- name: GetUserByHandle :one
SELECT * FROM users WHERE lower(handle) = lower(@handle);

-- name: RandomActiveHandle :one
--
-- One random claimed page, optionally skipping one account (the visitor's
-- own). ORDER BY random() scans and sorts every active row on each call: fine
-- at village scale. Once the table is large, switch to TABLESAMPLE or pick a
-- random value over a dense id range instead.
SELECT handle FROM users
WHERE state = 'active' AND handle IS NOT NULL
  AND (sqlc.narg(exclude_id)::uuid IS NULL OR id <> sqlc.narg(exclude_id)::uuid)
ORDER BY random() LIMIT 1;

-- name: SetDisplayName :one
--
-- A NULL display_name clears the name. The page falls back to the handle.
UPDATE users
SET display_name = sqlc.narg(display_name)
WHERE id = @id
RETURNING *;

-- name: SetPhoto :one
--
-- Replaces the account's photo and returns the key it replaced ('' when it had
-- none). One statement does the allowlist check, the once-per-day check and the
-- read of the old key, so two uploads racing cannot both win and the caller
-- always learns which file to delete.
--
-- Two uploads racing cannot both win: the loser's WHERE is re-checked against
-- the winner's committed photo_uploaded_at and matches nothing. Postgres does
-- that re-check on an UPDATE anyway; FOR UPDATE is not what makes it work. What
-- FOR UPDATE buys is that old_key, read in the subselect, is the latest value
-- once the row lock is granted, so the caller deletes the file the database
-- really replaced. No rows means "not allowed": not allowlisted, or already
-- uploaded since @day_start.
UPDATE users
SET photo_key = @photo_key,
    photo_width = @photo_width,
    photo_height = @photo_height,
    photo_uploaded_at = now()
FROM (SELECT u.id, u.photo_key AS old_key FROM users u WHERE u.id = @id FOR UPDATE) old
WHERE users.id = old.id
  AND users.can_upload_photos
  AND (users.photo_uploaded_at IS NULL OR users.photo_uploaded_at < @day_start)
RETURNING coalesce(old.old_key, '')::text AS old_key;

-- name: RemovePhoto :one
--
-- Clears the photo and returns the key it had ('' when none). photo_uploaded_at
-- stays, so removing does not reset the daily limit. Removing when there is no
-- photo matches the row and changes nothing, so it is a no-op and not an error.
UPDATE users
SET photo_key = NULL, photo_width = NULL, photo_height = NULL
FROM (SELECT u.id, u.photo_key AS old_key FROM users u WHERE u.id = @id FOR UPDATE) old
WHERE users.id = old.id
RETURNING coalesce(old.old_key, '')::text AS old_key;

-- name: GetUserByPhotoKey :one
SELECT * FROM users WHERE photo_key = @photo_key;
