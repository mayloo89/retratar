-- name: ReserveDeletedHandle :exec
--
-- Holds a deleted account's handle back. @handle is lower-cased here so no
-- caller can store another spelling; a handle deleted twice (claimed again after
-- the reservation ended) restarts the clock. @deleted_at comes from the
-- application's clock, the same one ClaimHandle measures the reservation by.
INSERT INTO deleted_handles (handle, deleted_at)
VALUES (lower(@handle::text), @deleted_at)
ON CONFLICT (handle) DO UPDATE SET deleted_at = @deleted_at;

-- name: PurgeDeletedHandles :execrows
--
-- Drops reservations that have run out. Measured on the database clock like the
-- other purges; ClaimHandle already ignores a reservation past its 90 days, so
-- this only keeps the table small.
DELETE FROM deleted_handles
WHERE deleted_at < now() - make_interval(secs => @older_than_seconds::double precision);
