-- name: CreateGuestbookEntry :one
INSERT INTO guestbook_entries (page_user_id, author_user_id, body)
VALUES (@page_user_id, @author_user_id, @body)
RETURNING *;

-- name: ListVisibleGuestbookEntries :many
--
-- Joins users for the author's handle so one query renders the whole list,
-- rather than a lookup per entry.
SELECT e.id, e.body, e.created_at, u.handle AS author_handle
FROM guestbook_entries e
JOIN users u ON u.id = e.author_user_id
WHERE e.page_user_id = @page_user_id AND e.state = 'visible'
ORDER BY e.created_at DESC
LIMIT @lim;

-- name: ListOwnGuestbookEntries :many
--
-- The owner's view of their own page: every state, so a hidden entry stays
-- listed and can be shown again.
SELECT e.id, e.body, e.state, e.created_at, u.handle AS author_handle
FROM guestbook_entries e
JOIN users u ON u.id = e.author_user_id
WHERE e.page_user_id = @page_user_id
ORDER BY e.created_at DESC
LIMIT @lim;

-- name: SetGuestbookEntryState :execrows
--
-- The page_user_id condition is the authorization check: an entry on someone
-- else's page matches no row, so the caller sees zero rows affected.
UPDATE guestbook_entries SET state = @state
WHERE id = @id AND page_user_id = @page_user_id;
