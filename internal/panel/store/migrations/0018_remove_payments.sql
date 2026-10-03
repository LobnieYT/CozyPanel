-- +goose Up
-- Manual payment instead of invoices: the payments table and the sale columns go away.
-- traffic_grants loses its link to payments (admin grants never had one); existing
-- grants keep their rows with the values they were given.
DROP TABLE IF EXISTS payments;
ALTER TABLE tariffs DROP COLUMN on_sale;
ALTER TABLE tariffs DROP COLUMN price_rub;
ALTER TABLE tariffs DROP COLUMN price_stars;
ALTER TABLE traffic_packages DROP COLUMN on_sale;
ALTER TABLE traffic_packages DROP COLUMN price_rub;
ALTER TABLE traffic_packages DROP COLUMN price_stars;
CREATE TABLE traffic_grants_new (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  pool_id    INTEGER REFERENCES traffic_pools(id) ON DELETE CASCADE,
  bytes      INTEGER NOT NULL CHECK (bytes > 0),
  remaining  INTEGER NOT NULL CHECK (remaining BETWEEN 0 AND bytes),
  lifetime   TEXT NOT NULL CHECK (lifetime IN ('used', 'period', 'days')),
  expires_at INTEGER,
  source     TEXT NOT NULL CHECK (source IN ('purchase', 'admin')),
  package_id INTEGER REFERENCES traffic_packages(id) ON DELETE SET NULL,
  note       TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
INSERT INTO traffic_grants_new (id, user_id, pool_id, bytes, remaining, lifetime, expires_at, source, package_id, note, created_at)
SELECT id, user_id, pool_id, bytes, remaining, lifetime, expires_at, source, package_id, note, created_at
FROM traffic_grants;
DROP TABLE traffic_grants;
ALTER TABLE traffic_grants_new RENAME TO traffic_grants;
CREATE INDEX traffic_grants_user ON traffic_grants(user_id, pool_id);

-- +goose Down
-- The sale columns come back empty; invoices and payment links do not.
ALTER TABLE tariffs ADD COLUMN price_stars INTEGER;
ALTER TABLE tariffs ADD COLUMN price_rub INTEGER;
ALTER TABLE tariffs ADD COLUMN on_sale INTEGER NOT NULL DEFAULT 0;
ALTER TABLE traffic_packages ADD COLUMN price_stars INTEGER;
ALTER TABLE traffic_packages ADD COLUMN price_rub INTEGER;
ALTER TABLE traffic_packages ADD COLUMN on_sale INTEGER NOT NULL DEFAULT 0;
