-- +goose Up

-- When an account accepted the terms and confirmed it is 18 or older, set in
-- the same statement that claims its handle. NULL means the account claimed its
-- handle before this column existed, or has not claimed one yet.
ALTER TABLE users ADD COLUMN terms_accepted_at timestamptz;

-- Handles of deleted accounts, held back for 90 days so nobody can take the
-- address of a page that was just deleted and pass as its owner. The handle is
-- stored in lower case, because handles are unique case-insensitively. The
-- table holds nothing else on purpose: no account id, no email. The hourly
-- purge deletes rows past the 90 days.
CREATE TABLE deleted_handles (
    handle     text PRIMARY KEY CHECK (handle = lower(handle)),
    deleted_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE deleted_handles;
ALTER TABLE users DROP COLUMN terms_accepted_at;
