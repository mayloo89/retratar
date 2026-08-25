-- +goose Up

-- A logged-in session. Same shape as login_tokens on purpose: the value that
-- lives in the client's cookie is never stored, only its SHA-256, so a dump
-- of this table cannot be replayed into anyone's account.
--
-- expires_at is set once at issue and never pushed forward. There is no
-- sliding renewal: a stolen session ages out on its own instead of staying
-- alive for as long as someone keeps using it.
CREATE TABLE sessions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_hash bytea NOT NULL,
    user_id     uuid NOT NULL REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz
);

-- Lookup is by hash and only by hash, same reasoning as login_tokens_hash_key.
CREATE UNIQUE INDEX sessions_hash_key ON sessions (session_hash);

-- Supports listing and revoking a user's sessions from a settings page later.
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- +goose Down
DROP TABLE sessions;
