-- +goose Up

-- Entries other people leave on a user's page. state exists from the start
-- because the next change (the owner hiding an entry) only flips it, and a
-- later moderation phase may add a 'pending' value; adding a column or
-- rewriting rows then would be a worse migration than allowing the values now.
--
-- Both foreign keys cascade. An entry belongs to two accounts: deleting the
-- page owner removes the page it sat on, and deleting the author removes
-- what they wrote, so no row is ever left pointing at an account that is gone.
CREATE TABLE guestbook_entries (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    author_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body           text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 280),
    state          text NOT NULL DEFAULT 'visible' CHECK (state IN ('visible', 'hidden')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK (page_user_id <> author_user_id)
);
CREATE INDEX guestbook_entries_page_idx ON guestbook_entries (page_user_id, created_at DESC);

-- +goose Down
DROP TABLE guestbook_entries;
