-- +goose Up

-- An account. Created when a login token is consumed, never when one is
-- requested, so an unanswered request leaves nothing behind.
CREATE TABLE users (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email      text NOT NULL,
    handle     text,
    state      text NOT NULL DEFAULT 'pending_handle',
    tier       text NOT NULL DEFAULT 'free',
    locale     text NOT NULL DEFAULT 'es-AR',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Email is the login identity, so it is unique, and unique without regard to
-- case: nobody believes Ana@ and ana@ are two accounts. The application also
-- normalises before writing. This index is the net, not the mechanism.
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

-- Partial, because the handle is chosen after the first login and is NULL until
-- then. A plain unique index would be fine for NULLs under the SQL standard,
-- but the predicate says the intent out loud.
CREATE UNIQUE INDEX users_handle_key ON users (lower(handle)) WHERE handle IS NOT NULL;

-- A single-use magic link.
--
-- The token itself is never stored, only its SHA-256. A dump of this table
-- cannot be replayed into anyone's account. The column is bytea rather than
-- text so there is no encoding to get wrong on either side.
CREATE TABLE login_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash  bytea NOT NULL,
    email       text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz
);

-- Lookup is by hash and only by hash. Unique so that a hash collision, however
-- theoretical, surfaces as a write error rather than an ambiguous login.
CREATE UNIQUE INDEX login_tokens_hash_key ON login_tokens (token_hash);

-- Supports invalidating a user's outstanding tokens when they request a new
-- link, and later, counting recent requests for rate limiting.
CREATE INDEX login_tokens_email_idx ON login_tokens (email, created_at DESC);

-- +goose Down
DROP TABLE login_tokens;
DROP TABLE users;
