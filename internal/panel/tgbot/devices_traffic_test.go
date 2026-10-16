package tgbot

import (
	"context"
	"strings"
	"testing"
)

// The devices screen shows each bound device's traffic since it was bound, and
// no traffic token for a device that used nothing.
func TestDevicesScreenShowsTraffic(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := e.st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("INSERT INTO slots (id, name, uuid, secret, state, created_at) VALUES (424241, 's424241', 'uu1', 'x', 'assigned', 1), (424242, 's424242', 'uu2', 'x', 'assigned', 1)")
	exec("INSERT INTO bound_devices (user_id, hwid, slot_id, os, model, created_at, last_seen) VALUES (?, 'hw1', 424241, 'Android', 'Pixel 9', 1, 1)", e.user.ID)
	exec("INSERT INTO bound_devices (user_id, hwid, slot_id, os, model, created_at, last_seen) VALUES (?, 'hw2', 424242, 'iOS', 'iPhone', 1, 1)", e.user.ID)
	exec("INSERT INTO slot_traffic (slot_id, up, down) VALUES (424241, 150, 950)")

	w := wordsFor("ru")
	text, _ := e.bot.devices(ctx, w, e.user, "d", 0, "", e.now)
	token := w.bytes(1100)
	if !strings.Contains(text, token) {
		t.Fatalf("no traffic token %q in:\n%s", token, text)
	}
	if got := strings.Count(text, token); got != 1 {
		t.Fatalf("traffic token %d times, want 1:\n%s", got, text)
	}
}

// The devices screen calls a renamed device by its own name.
func TestDevicesScreenShowsNames(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := e.st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("INSERT INTO slots (id, name, uuid, secret, state, created_at) VALUES (424241, 's424241', 'uu1', 'x', 'assigned', 1)")
	exec("INSERT INTO bound_devices (user_id, hwid, slot_id, os, model, name, created_at, last_seen) VALUES (?, 'hw1', 424241, 'Android', 'Pixel 9', 'Мамин телефон', 1, 1)", e.user.ID)

	w := wordsFor("ru")
	text, _ := e.bot.devices(ctx, w, e.user, "d", 0, "", e.now)
	if !strings.Contains(text, "Мамин телефон") {
		t.Fatalf("no own name in:\n%s", text)
	}
	if strings.Contains(text, "Pixel 9") {
		t.Fatalf("the app name leaks beside the own one:\n%s", text)
	}
}
