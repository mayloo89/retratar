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
