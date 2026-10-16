package store

import (
	"context"
	"database/sql"
	"testing"

	"cozy/internal/panel/store/db"
)

// Device names and bans: rename sticks, banning twice keeps one row with the
// latest label, the admin's name comes along, unbanning drops the row.
func TestDeviceNamesBans(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("INSERT INTO slots (id, name, uuid, secret, state, created_at) VALUES (1, 's000001', 'u1', 'x', 'assigned', 1)")
	exec("INSERT INTO users (id, name, sub_token, period_start, created_at, updated_at) VALUES (1, 'a', 'tok', 1, 1, 1)")
	exec("INSERT INTO admins (id, username, password_hash, created_at) VALUES (1, 'root', 'x', 1)")
	exec("INSERT INTO bound_devices (id, user_id, hwid, slot_id, model, created_at, last_seen) VALUES (1, 1, 'hw1', 1, 'Pixel 9', 1, 1)")

	n, err := st.Q.SetBoundDeviceName(ctx, db.SetBoundDeviceNameParams{Name: "планшет", ID: 1, UserID: 1})
	if err != nil || n != 1 {
		t.Fatalf("rename rows %d (%v), want 1", n, err)
	}
	if n, err := st.Q.SetBoundDeviceName(ctx, db.SetBoundDeviceNameParams{Name: "x", ID: 1, UserID: 2}); err != nil || n != 0 {
		t.Fatalf("foreign rename rows %d (%v), want 0", n, err)
	}
	dev, err := st.Q.GetBoundDevice(ctx, db.GetBoundDeviceParams{UserID: 1, Hwid: "hw1"})
	if err != nil || dev.Name != "планшет" {
		t.Fatalf("the name after rename: %q %v", dev.Name, err)
	}

	mkban := func(label string, at int64) db.DeviceBan {
		t.Helper()
		ban, err := st.Q.CreateDeviceBan(ctx, db.CreateDeviceBanParams{UserID: 1, Hwid: "hw1", Label: label,
			AdminID: sql.NullInt64{Int64: 1, Valid: true}, BannedAt: at})
		if err != nil {
			t.Fatalf("ban %q: %v", label, err)
		}
		return ban
	}
	mkban("Телефон", 2)
	second := mkban("Планшет", 3)
	if second.Label != "Планшет" || second.BannedAt != 3 {
		t.Fatalf("the second ban did not refresh the row: %+v", second)
	}
	var rows int
	if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM device_bans").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("ban rows: %d %v, want 1", rows, err)
	}
	if _, err := st.Q.ActiveDeviceBan(ctx, db.ActiveDeviceBanParams{UserID: 1, Hwid: "hw1"}); err != nil {
		t.Fatalf("active ban: %v", err)
	}
	if _, err := st.Q.ActiveDeviceBan(ctx, db.ActiveDeviceBanParams{UserID: 1, Hwid: "hw2"}); err == nil {
		t.Fatal("a ban of a device that was never banned")
	}
	bans, err := st.Q.ListDeviceBans(ctx, 1)
	if err != nil || len(bans) != 1 || bans[0].Label != "Планшет" || bans[0].AdminName != "root" {
		t.Fatalf("bans list: %+v %v", bans, err)
	}
	if _, err := st.Q.DeleteDeviceBan(ctx, db.DeleteDeviceBanParams{ID: bans[0].ID, UserID: 1}); err != nil {
		t.Fatalf("unban: %v", err)
	}
	if _, err := st.Q.DeleteDeviceBan(ctx, db.DeleteDeviceBanParams{ID: bans[0].ID, UserID: 1}); err == nil {
		t.Fatal("unbanning twice went through")
	}
}
