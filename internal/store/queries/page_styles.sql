-- name: GetPageStyle :one
SELECT * FROM page_styles WHERE user_id = @user_id;

-- name: UpsertPageStyle :exec
--
-- The columns are in the table's order on purpose: internal/user converts a
-- store.PageStyle straight into these params.
INSERT INTO page_styles (
    user_id, bg, sheet, text_color, link, name, bar_bg, bar_fg,
    font_head, font_body, own_mood_box, updated_at
) VALUES (
    @user_id, @bg, @sheet, @text_color, @link, @name, @bar_bg, @bar_fg,
    @font_head, @font_body, @own_mood_box, @updated_at
)
ON CONFLICT (user_id) DO UPDATE SET
    bg = EXCLUDED.bg, sheet = EXCLUDED.sheet, text_color = EXCLUDED.text_color,
    link = EXCLUDED.link, name = EXCLUDED.name, bar_bg = EXCLUDED.bar_bg,
    bar_fg = EXCLUDED.bar_fg, font_head = EXCLUDED.font_head,
    font_body = EXCLUDED.font_body, own_mood_box = EXCLUDED.own_mood_box,
    updated_at = EXCLUDED.updated_at;

-- name: DeletePageStyle :exec
DELETE FROM page_styles WHERE user_id = @user_id;
