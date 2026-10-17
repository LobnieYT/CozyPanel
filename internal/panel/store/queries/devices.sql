-- name: GetBoundDevice :one
SELECT * FROM bound_devices WHERE user_id = ? AND hwid = ?;

-- name: GetBoundDeviceByID :one
SELECT * FROM bound_devices WHERE id = ? AND user_id = ?;

-- name: ListBoundDevices :many
SELECT * FROM bound_devices WHERE user_id = ? ORDER BY created_at, id;

-- name: ListIdleBoundDevices :many
SELECT * FROM bound_devices WHERE last_seen < ?;

-- name: CountBoundDevices :one
SELECT count(*) FROM bound_devices WHERE user_id = ?;

-- name: CreateBoundDevice :one
INSERT INTO bound_devices (user_id, hwid, slot_id, os, os_version, model, app, last_ip, created_at, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: TouchBoundDevice :exec
UPDATE bound_devices SET os = ?, os_version = ?, model = ?, app = ?, last_ip = ?, last_seen = ? WHERE id = ?;

-- name: DeleteBoundDevice :exec
DELETE FROM bound_devices WHERE id = ?;

-- name: DeleteBoundDevicesOf :exec
DELETE FROM bound_devices WHERE user_id = ?;

-- name: SetUserSlot :exec
-- A new own slot for the user, same subscription link (the shared device was unbound).
UPDATE users SET slot_id = ?, updated_at = ? WHERE id = ?;

-- name: AddUnbind :exec
-- The subscriber's own unbind, for the admin's limit of so many a period.
INSERT INTO device_unbinds (user_id, at) VALUES (?, ?);

-- name: AddLinkReissue :exec
-- The subscriber's own new link, for the admin's limit of so many a period.
INSERT INTO link_reissues (user_id, at) VALUES (?, ?);

-- name: ListLinkReissuesSince :many
SELECT at FROM link_reissues WHERE user_id = ? AND at > ? ORDER BY at;

-- name: DeleteLinkReissuesBefore :exec
DELETE FROM link_reissues WHERE at < ?;

-- name: ListUnbindsSince :many
SELECT at FROM device_unbinds WHERE user_id = ? AND at >= ? ORDER BY at;

-- name: DeleteUnbindsBefore :exec
DELETE FROM device_unbinds WHERE at < ?;

-- name: DeleteExpiredBans :exec
-- Pauses that ran out: the admin's bans (no until) stay.
DELETE FROM device_bans WHERE until IS NOT NULL AND until <= ?;

-- name: ListDeviceSlots :many
-- Slots of bound devices with an id: keys of their own, profile fetches of their own.
SELECT s.name AS slot_name, d.user_id, d.last_seen FROM bound_devices d JOIN slots s ON s.id = d.slot_id WHERE d.hwid != '';

-- name: AddSlotTraffic :exec
-- Adds one batch to its slot; an unknown slot name inserts nothing, so slots
-- of nobody never leave a row behind.
INSERT INTO slot_traffic (slot_id, up, down)
SELECT id, ?, ? FROM slots WHERE name = ?
ON CONFLICT (slot_id) DO UPDATE SET up = slot_traffic.up + excluded.up, down = slot_traffic.down + excluded.down;

-- name: ListBoundDeviceTraffic :many
-- What each of a user's bound devices used since it was bound.
SELECT d.id AS device_id, COALESCE(t.up, 0) AS up, COALESCE(t.down, 0) AS down
FROM bound_devices d LEFT JOIN slot_traffic t ON t.slot_id = d.slot_id WHERE d.user_id = ?;

-- name: SetBoundDeviceName :execrows
-- The device's own name; '' goes back to the one its app reports.
UPDATE bound_devices SET name = ? WHERE id = ? AND user_id = ?;

-- name: CreateDeviceBan :one
-- Banning twice keeps one row: the label, the admin and the moment are the latest.
INSERT INTO device_bans (user_id, hwid, label, admin_id, banned_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (user_id, hwid) DO UPDATE SET label = excluded.label, admin_id = excluded.admin_id, banned_at = excluded.banned_at
RETURNING *;

-- name: ActiveDeviceBan :one
-- The ban on a device: until is null for the admin's ban, a moment for a pause.
SELECT until FROM device_bans WHERE user_id = ? AND hwid = ? AND (until IS NULL OR until > ?);

-- name: ListDeviceBans :many
-- The devices banned from a subscription, latest first; pauses that ran out are hidden.
SELECT b.id, b.user_id, b.hwid, b.label, b.admin_id, b.banned_at, b.until, COALESCE(a.username, '') AS admin_name
FROM device_bans b LEFT JOIN admins a ON a.id = b.admin_id
WHERE b.user_id = ? AND (b.until IS NULL OR b.until > ?) ORDER BY b.banned_at DESC, b.id DESC;

-- name: DeleteDeviceBan :one
DELETE FROM device_bans WHERE id = ? AND user_id = ? RETURNING *;

-- name: HoldUnbound :exec
-- A pause after the subscriber's own unbind: the device may not take a place again at
-- once. The admin's ban (no until) stays untouched.
INSERT INTO device_bans (user_id, hwid, label, banned_at, until)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (user_id, hwid) DO UPDATE SET label = excluded.label, banned_at = excluded.banned_at, until = excluded.until
WHERE device_bans.until IS NOT NULL;
