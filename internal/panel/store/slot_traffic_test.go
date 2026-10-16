package store

import (
	"context"
	"testing"

	"cozy/internal/panel/store/db"
)

// Slot traffic accumulates per batch, resolves slots by name, and leaves no row
// behind for a slot name nobody owns.
func TestSlotTrafficAccumulates(t *testing.T) {
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
	exec("INSERT INTO slots (id, name, uuid, secret, state, created_at) VALUES (1, 's000001', 'u1', 'x', 'assigned', 1), (2, 's000002', 'u2', 'x', 'assigned', 1)")
	exec("INSERT INTO users (id, name, sub_token, period_start, created_at, updated_at) VALUES (1, 'a', 'tok', 1, 1, 1)")
	exec("INSERT INTO bound_devices (user_id, hwid, slot_id, created_at, last_seen) VALUES (1, 'hw1', 1, 1, 1), (1, 'hw2', 2, 1, 1)")

	add := func(name string, up, down int64) {
		t.Helper()
		if err := st.Q.AddSlotTraffic(ctx, db.AddSlotTrafficParams{Up: up, Down: down, Name: name}); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	add("s000001", 100, 900)
	add("s000001", 50, 50)
	add("s000002", 7, 8)
	add("s999999", 5, 5)

	rows, err := st.Q.ListBoundDeviceTraffic(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64][2]int64{}
	for _, r := range rows {
		got[r.DeviceID] = [2]int64{r.Up, r.Down}
	}
	if len(rows) != 2 {
		t.Fatalf("traffic rows: %d, want 2", len(rows))
	}
	var d1, d2 int64
	if err := st.DB.QueryRowContext(ctx, "SELECT id FROM bound_devices WHERE hwid = 'hw1'").Scan(&d1); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRowContext(ctx, "SELECT id FROM bound_devices WHERE hwid = 'hw2'").Scan(&d2); err != nil {
		t.Fatal(err)
	}
	if got[d1] != [2]int64{150, 950} {
		t.Fatalf("device hw1: %v, want [150 950]", got[d1])
	}
	if got[d2] != [2]int64{7, 8} {
		t.Fatalf("device hw2: %v, want [7 8]", got[d2])
	}
	var n int
	if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM slot_traffic").Scan(&n); err != nil || n != 2 {
		t.Fatalf("slot_traffic rows: %d %v, want 2 (nobody's slot leaves nothing)", n, err)
	}
}
