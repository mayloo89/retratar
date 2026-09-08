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
