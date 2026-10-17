-- +goose Up
-- The subscriber's own new links, for the admin's limit of so many a period.
CREATE TABLE link_reissues (
  id      INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  at      INTEGER NOT NULL
);
CREATE INDEX link_reissues_user_at ON link_reissues (user_id, at);

-- +goose Down
DROP TABLE link_reissues;