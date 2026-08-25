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
