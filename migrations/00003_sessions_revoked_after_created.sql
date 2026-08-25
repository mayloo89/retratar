-- +goose Up

-- Guards against a bad revoke path setting revoked_at earlier than the
-- session was ever created, which would make the session look revoked
-- before it existed.
ALTER TABLE sessions ADD CONSTRAINT sessions_revoked_after_created_at
    CHECK (revoked_at IS NULL OR revoked_at >= created_at);

-- +goose Down
ALTER TABLE sessions DROP CONSTRAINT sessions_revoked_after_created_at;
