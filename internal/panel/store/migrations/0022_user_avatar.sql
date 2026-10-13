-- +goose Up
-- A user's avatar: the file under data/avatars, shown in the panel.
ALTER TABLE users ADD COLUMN avatar TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite cannot drop a column on old versions without a rebuild; the column
-- stays, the panel ignores it.
SELECT 1;
