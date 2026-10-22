-- name: CollectStats :one
--
-- The operator's numbers, aggregates only. Every time is measured from @now,
-- never now(), so a test can pin the clock. "7d" is the closed window
-- [@now - 7 days, @now].
SELECT
    (SELECT count(*) FROM users u)::bigint AS accounts,
    (SELECT count(*) FROM users u WHERE u.handle IS NOT NULL)::bigint AS pages,
    (SELECT count(DISTINCT s.user_id) FROM sessions s
        WHERE s.created_at >= sqlc.arg(now)::timestamptz - interval '7 days')::bigint AS signed_in_7d,

    (SELECT count(*) FROM mood_history mh
        WHERE mh.at >= sqlc.arg(now)::timestamptz - interval '7 days')::bigint AS mood_changes_7d,
    (SELECT count(DISTINCT mh.user_id) FROM mood_history mh
        WHERE mh.at >= sqlc.arg(now)::timestamptz - interval '7 days')::bigint AS changed_mood_7d,

    -- Week two: a page is eligible once its account is 14 days old, and passes
    -- with 2+ history rows in [created_at + 7 days, created_at + 14 days).
    -- mood_history, not moods: moods keeps only the latest change.
    (SELECT count(*) FROM users u
        WHERE u.handle IS NOT NULL
          AND u.created_at <= sqlc.arg(now)::timestamptz - interval '14 days')::bigint AS week_two_eligible,
    (SELECT count(*) FROM users u
        WHERE u.handle IS NOT NULL
          AND u.created_at <= sqlc.arg(now)::timestamptz - interval '14 days'
          AND (SELECT count(*) FROM mood_history h
                WHERE h.user_id = u.id
                  AND h.at >= u.created_at + interval '7 days'
                  AND h.at <  u.created_at + interval '14 days') >= 2)::bigint AS week_two_passed,

    (SELECT count(*) FROM guestbook_entries ge)::bigint AS guestbook_total,
    (SELECT count(*) FROM guestbook_entries ge
        WHERE ge.created_at >= sqlc.arg(now)::timestamptz - interval '7 days')::bigint AS guestbook_7d,

    -- Unordered pairs counted once: a < b picks one direction, DISTINCT
    -- collapses several entries between the same two accounts. Any state.
    (SELECT count(*) FROM (
        SELECT DISTINCT e1.page_user_id, e1.author_user_id
        FROM guestbook_entries e1
        JOIN guestbook_entries e2
          ON e2.page_user_id = e1.author_user_id
         AND e2.author_user_id = e1.page_user_id
        WHERE e1.page_user_id < e1.author_user_id
    ) pairs)::bigint AS reciprocal_pairs,

    (SELECT count(*) FROM users u WHERE u.photo_key IS NOT NULL)::bigint AS photos_now,
    (SELECT count(*) FROM users u
        WHERE u.photo_uploaded_at >= sqlc.arg(now)::timestamptz - interval '7 days')::bigint AS photo_uploads_7d,
    (SELECT count(*) FROM users u WHERE u.can_upload_photos)::bigint AS allowlisted;
