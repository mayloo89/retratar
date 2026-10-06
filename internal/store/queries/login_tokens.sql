-- name: CreateLoginToken :exec
--
-- expires_at is computed by the database rather than passed in. The expiry check
-- in ConsumeLoginToken reads the database clock, so the deadline has to come
-- from that same clock: taken from the application host instead, any skew
-- between the two silently lengthens or shortens every token's life.
INSERT INTO login_tokens (token_hash, email, expires_at)
VALUES (@token_hash, @email, now() + make_interval(secs => @ttl_seconds::double precision));

-- name: InvalidateLoginTokensForEmail :exec
--
-- Requesting a new link retires the outstanding ones, so at most one link for an
-- address works at a time. Cheap, and it bounds what an attacker gains from a
-- mailbox they can read but not control.
UPDATE login_tokens
SET    consumed_at = now()
WHERE  email = @email
  AND  consumed_at IS NULL;

-- name: ConsumeLoginToken :one
--
-- Spends a token and reports who it belongs to. Single-use is enforced here, in
-- one statement, and not by reading the row and then updating it: two requests
-- arriving together would both pass the read and both proceed. The UPDATE takes
-- a row lock, so exactly one of them matches consumed_at IS NULL.
--
-- No row means the token does not exist, has expired, or has already been spent.
-- The caller cannot tell which, and should not: reporting the difference tells
-- an attacker which of their guesses were once real tokens.
UPDATE login_tokens
SET    consumed_at = now()
WHERE  token_hash = @token_hash
  AND  consumed_at IS NULL
  AND  expires_at > now()
RETURNING email;

-- name: CountRecentLoginTokens :one
--
-- Counts every link minted for the address inside the window, spent or not:
-- the budget is on emails sent, not on links outstanding. Served by
-- login_tokens_email_idx (email, created_at DESC). The window is measured on
-- the database clock, like the expiry, so both come from one time source.
SELECT count(*) FROM login_tokens
WHERE  email = @email
  AND  created_at > now() - make_interval(secs => @window_seconds::double precision);

-- name: LockLoginAddress :exec
--
-- Transaction-scoped advisory lock on the address, taken before counting so two
-- concurrent requests cannot both read count=2 and both mint. Released at
-- commit or rollback; requests for other addresses do not contend.
SELECT pg_advisory_xact_lock(hashtextextended(@email::text, 0));

-- name: PurgeLoginTokens :execrows
--
-- The cut-off is measured from expiry, not consumption, so a spent token is kept
-- as long as an unspent one. The sign-in budget (CountRecentLoginTokens) counts
-- spent rows too, so purging by consumed_at or created_at could delete rows the
-- budget still needs.
DELETE FROM login_tokens
WHERE expires_at < now() - make_interval(secs => @older_than_seconds::double precision);
