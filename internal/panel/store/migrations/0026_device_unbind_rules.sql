-- +goose Up
-- The subscriber's own unbinds, for the admin's limit of so many a period: the old
-- users.unbound_at kept only the last one, enough for "one a day" and no more.
CREATE TABLE device_unbinds (
  id      INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  at      INTEGER NOT NULL
);
CREATE INDEX device_unbinds_user_at ON device_unbinds (user_id, at);
INSERT INTO device_unbinds (user_id, at) SELECT id, unbound_at FROM users WHERE unbound_at > 0;
ALTER TABLE users DROP COLUMN unbound_at;

-- A ban until a moment: a device its subscriber unbound may not take a place again at
-- once (its app, still open, would fetch the subscription and bind again). NULL: for
-- good, the admin's ban.
ALTER TABLE device_bans ADD COLUMN until INTEGER;

-- How the device limit counts: bound places, or addresses online at once.
ALTER TABLE tariffs ADD COLUMN device_mode TEXT NOT NULL DEFAULT 'bound' CHECK (device_mode IN ('bound', 'online'));
ALTER TABLE users ADD COLUMN device_mode TEXT NOT NULL DEFAULT 'bound' CHECK (device_mode IN ('bound', 'online'));

-- +goose Down
ALTER TABLE users DROP COLUMN device_mode;
ALTER TABLE tariffs DROP COLUMN device_mode;
DELETE FROM device_bans WHERE until IS NOT NULL;
ALTER TABLE device_bans DROP COLUMN until;
ALTER TABLE users ADD COLUMN unbound_at INTEGER NOT NULL DEFAULT 0;
UPDATE users SET unbound_at = (SELECT max(at) FROM device_unbinds WHERE user_id = users.id) WHERE id IN (SELECT user_id FROM device_unbinds);
DROP TABLE device_unbinds;
