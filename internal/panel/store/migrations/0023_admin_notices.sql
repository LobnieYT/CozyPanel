-- +goose Up
-- Resource alerts dedup: one row per node, kind and day, so a restart does not
-- resend what already went out.
CREATE TABLE admin_notices (
  node_id INTEGER NOT NULL,
  kind    TEXT NOT NULL,    -- cpu_high | mem_high
  period  INTEGER NOT NULL, -- unix day number
  sent_at INTEGER NOT NULL,
  PRIMARY KEY (node_id, kind, period)
) WITHOUT ROWID;

-- +goose Down
DROP TABLE admin_notices;
