-- +goose Up
-- Sub-admins of the panel: a validity window, per-section grants and the owner
-- flag. The first admin becomes the owner: full rights, no expiry, irremovable.
ALTER TABLE admins ADD COLUMN disabled_at INTEGER;
ALTER TABLE admins ADD COLUMN expires_at INTEGER;
ALTER TABLE admins ADD COLUMN scopes TEXT NOT NULL DEFAULT '{}';
ALTER TABLE admins ADD COLUMN is_owner INTEGER NOT NULL DEFAULT 0;
UPDATE admins SET is_owner = 1 WHERE id = (SELECT MIN(id) FROM admins);

-- +goose Down
ALTER TABLE admins DROP COLUMN is_owner;
ALTER TABLE admins DROP COLUMN scopes;
ALTER TABLE admins DROP COLUMN expires_at;
ALTER TABLE admins DROP COLUMN disabled_at;
