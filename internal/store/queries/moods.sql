-- name: UpsertMood :one
--
-- The mood a page shows is a single current value, never a history of rows
-- to pick the latest from — the same DO UPDATE shape as UpsertUserByEmail,
-- for the same reason: one row per user, always.
INSERT INTO moods (user_id, mood_key, note, updated_at)
VALUES (@user_id, @mood_key, @note, now())
ON CONFLICT (user_id) DO UPDATE
    SET mood_key = excluded.mood_key, note = excluded.note, updated_at = now()
RETURNING *;

-- name: GetMoodByUserID :one
SELECT * FROM moods WHERE user_id = @user_id;

-- name: InsertMoodHistory :exec
--
-- Append-only, one row per change. Nothing reads this yet; it exists so a
-- future "tu mes en moods" view is a query, not a migration.
INSERT INTO mood_history (user_id, mood_key, note) VALUES (@user_id, @mood_key, @note);
