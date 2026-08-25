-- name: CreateSession :exec
--
-- expires_at is computed by the database for the same reason as
-- CreateLoginToken: the expiry check in LookupSession reads the database
-- clock, so the deadline has to come from that clock too.
INSERT INTO sessions (session_hash, user_id, expires_at)
VALUES (@session_hash, @user_id, now() + make_interval(secs => @ttl_seconds::double precision));

-- name: LookupSession :one
--
-- No row means the session does not exist, has expired, or was revoked. The
-- caller cannot tell which, for the same reason ConsumeLoginToken merges its
-- three causes: the difference is only useful to whoever is guessing.
SELECT user_id FROM sessions
WHERE  session_hash = @session_hash
  AND  revoked_at IS NULL
  AND  expires_at > now();

-- name: RevokeSession :exec
--
-- Revoking twice is not an error: logout should look like it worked whether
-- or not the session was still live, so the caller never needs to check
-- rows-affected before clearing the cookie.
UPDATE sessions
SET    revoked_at = now()
WHERE  session_hash = @session_hash
  AND  revoked_at IS NULL;
