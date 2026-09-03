-- +goose Up

-- The mood a page currently shows. One row per user; upserted on every
-- change, never inserted fresh, so "when did they last feel this" is just
-- updated_at.
CREATE TABLE moods (
    user_id    uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    mood_key   text NOT NULL CHECK (mood_key IN (
        'feliz', 'triste', 'tranquilo', 'ansioso', 'enamorado', 'cansado',
        'enojado', 'aburrido', 'inspirado', 'nostalgico', 'fiesta', 'perdido'
    )),
    note       text,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Every mood a user has ever set, oldest first per user. Nothing reads this
-- yet — no v1 block shows it — but the shape is settled now so "tu mes en
-- moods" is a query away later, not a migration away.
CREATE TABLE mood_history (
    id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    mood_key text NOT NULL,
    note     text,
    at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mood_history_user_id_at_idx ON mood_history (user_id, at DESC);

-- +goose Down
DROP TABLE mood_history;
DROP TABLE moods;
