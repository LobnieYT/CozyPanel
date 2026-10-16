package store

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// Migration 0017 on a database that has lived through 0016 (with 0018-0019 on top): the
// devices keep their rows without the unused column, the slot counter starts at the
// largest slot number, and the indexes are there.
func TestMigration0017KeepsWhatIsThere(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "old.db"))+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, conn, fsys)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("INSERT INTO slots (id, name, uuid, secret, state, created_at) VALUES (7, 's000007', 'u7', 'x', 'free', 1), (9, 's000009', 'u9', 'x', 'burned', 1)")
	exec("INSERT INTO users (id, name, sub_token, period_start, created_at, updated_at) VALUES (1, 'a', 'tok', 1, 1, 1)")
	exec("INSERT INTO devices (user_id, ip, first_seen, last_seen, client) VALUES (1, '203.0.113.5', 10, 20, 'old client')")

	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var ip string
	var last int64
	if err := conn.QueryRowContext(ctx, "SELECT ip, last_seen FROM devices WHERE user_id = 1").Scan(&ip, &last); err != nil || ip != "203.0.113.5" || last != 20 {
		t.Fatalf("the device after the migration: %q %d %v", ip, last, err)
	}
	if _, err := conn.ExecContext(ctx, "SELECT client FROM devices"); err == nil {
		t.Fatal("devices.client is still there")
	}
	var counter int64
	if err := conn.QueryRowContext(ctx, "SELECT last FROM slot_counter WHERE id = 1").Scan(&counter); err != nil || counter != 9 {
		t.Fatalf("the slot counter starts at %d (%v), want the largest slot number 9", counter, err)
	}
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name IN ('traffic_hourly_hour', 'traffic_daily_day', 'devices_last_seen', 'audit_log_ts')").Scan(&n); err != nil || n != 4 {
		t.Fatalf("time indexes: %d %v", n, err)
	}
	// And back: every rollback leaves the data, down to 0016.
	for {
		v, err := p.GetDBVersion(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if v <= 16 {
			break
		}
		if _, err := p.Down(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.QueryRowContext(ctx, "SELECT client FROM devices WHERE user_id = 1").Scan(new(string)); err != nil {
		t.Fatalf("the column after a rollback: %v", err)
	}
}

// Migration 0024 on a database that has lived through 0023: the slot_traffic
// table appears with zero defaults, follows its slot on delete, and the
// rollback drops the table while keeping the slots and devices.
func TestMigration0024SlotTraffic(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "old.db"))+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, conn, fsys)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 23); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("INSERT INTO slots (id, name, uuid, secret, state, created_at) VALUES (7, 's000007', 'u7', 'x', 'assigned', 1)")
	exec("INSERT INTO slots (id, name, uuid, secret, state, created_at) VALUES (9, 's000009', 'u9', 'x', 'assigned', 1)")
	exec("INSERT INTO users (id, name, sub_token, period_start, created_at, updated_at) VALUES (1, 'a', 'tok', 1, 1, 1)")
	exec("INSERT INTO bound_devices (user_id, hwid, slot_id, created_at, last_seen) VALUES (1, 'hw1', 7, 1, 1)")

	if _, err := p.UpTo(ctx, 24); err != nil {
		t.Fatal(err)
	}
	var up, down int64
	exec("INSERT INTO slot_traffic (slot_id, up, down) VALUES (7, 100, 900)")
	if err := conn.QueryRowContext(ctx, "SELECT up, down FROM slot_traffic WHERE slot_id = 7").Scan(&up, &down); err != nil || up != 100 || down != 900 {
		t.Fatalf("the slot traffic: %d %d %v", up, down, err)
	}
	exec("INSERT INTO slot_traffic (slot_id) VALUES (9)")
	if err := conn.QueryRowContext(ctx, "SELECT up, down FROM slot_traffic WHERE slot_id = 9").Scan(&up, &down); err != nil || up != 0 || down != 0 {
		t.Fatalf("zero defaults: %d %d %v", up, down, err)
	}
	// The traffic follows its slot: deleting the slot deletes its row only.
	exec("DELETE FROM bound_devices WHERE slot_id = 7")
	exec("DELETE FROM slots WHERE id = 7")
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM slot_traffic WHERE slot_id = 7").Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows of the deleted slot: %d %v", n, err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM slot_traffic").Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows of the surviving slot: %d %v", n, err)
	}
	// And back: the rollback drops the table, the slots survive it.
	if _, err := p.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SELECT * FROM slot_traffic"); err == nil {
		t.Fatal("slot_traffic is still there after the rollback")
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM slot_traffic").Scan(&n); err != nil {
		t.Fatalf("slot_traffic after migrate up again: %v", err)
	}
}
