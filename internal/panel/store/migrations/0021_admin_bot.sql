-- +goose Up
-- The admin bot: which Telegram account is linked to which admin, and the
-- requests of sub-admins waiting for the owner's approval. Subscriber links
-- (tg_links) stay untouched: admins live in admins, not in users.
CREATE TABLE tg_admin_chats (
  tg_id      INTEGER PRIMARY KEY, -- Telegram user id (= private chat id)
  admin_id   INTEGER NOT NULL UNIQUE REFERENCES admins(id) ON DELETE CASCADE,
  username   TEXT NOT NULL DEFAULT '',
  first_name TEXT NOT NULL DEFAULT '',
  blocked    INTEGER NOT NULL DEFAULT 0, -- the admin blocked the bot: nothing is sent
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE tg_admin_chats;
