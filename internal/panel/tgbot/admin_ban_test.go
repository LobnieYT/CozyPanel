package tgbot

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"

	"cozy/internal/panel/store/db"
)

// The admin blocks a device from the bot: pick, confirm, apply, the device is gone
// and its id is banned; the bans list unblocks it back.
func TestAdminDeviceBanUnban(t *testing.T) {
	bot, tg, chat, uid := usersEnv(t)
	ctx := context.Background()
	conn := bot.d.Store.DB
	if _, err := conn.ExecContext(ctx, "INSERT INTO slots (id, name, uuid, secret, state, created_at) VALUES (424241, 's424241', 'uu1', 'x', 'assigned', 1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO bound_devices (user_id, hwid, slot_id, os, model, created_at, last_seen) VALUES (?, 'hw1', 424241, 'Android', 'Pixel 9', 1, 1)", uid); err != nil {
		t.Fatal(err)
	}
	devs, err := bot.d.Store.Q.ListBoundDevices(ctx, uid)
	if err != nil || len(devs) != 1 {
		t.Fatalf("bound: %+v %v", devs, err)
	}
	uidStr := strconv.FormatInt(uid, 10)
	devStr := strconv.FormatInt(devs[0].ID, 10)

	n := tg.count()
	userPress(tg, chat, "ab:ban:"+uidStr)
	calls := tg.wait(t, n, "sendMessage")
	send, ok := find(calls, "sendMessage")
	if !ok {
		t.Fatal("no pick message")
	}
	want := "ab:cf:ban:" + uidStr + ":" + devStr
	found := false
	for _, data := range buttons(send) {
		if data == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("no ban button in %v", buttons(send))
	}
	n = tg.count()
	userPress(tg, chat, want)
	calls = tg.wait(t, n, "sendMessage")
	send, _ = find(calls, "sendMessage")
	if body := text(send); !strings.Contains(body, "Заблокировать устройство") {
		t.Fatalf("confirm: %q", body)
	}
	n = tg.count()
	userPress(tg, chat, "ab:do:ban:"+uidStr+":"+devStr)
	calls = tg.wait(t, n, "sendMessage")
	send, _ = find(calls, "sendMessage")
	if body := text(send); !strings.Contains(body, "заблокировано") {
		t.Fatalf("done: %q", body)
	}
	if n, _ := bot.d.Store.Q.CountBoundDevices(ctx, uid); n != 0 {
		t.Fatalf("bound after the ban: %d", n)
	}
	bans, err := bot.d.Store.Q.ListDeviceBans(ctx, db.ListDeviceBansParams{UserID: uid, Until: sql.NullInt64{Int64: 1 << 60, Valid: true}})
	if err != nil || len(bans) != 1 || bans[0].Hwid != "hw1" {
		t.Fatalf("bans: %+v %v", bans, err)
	}

	n = tg.count()
	userPress(tg, chat, "ab:unbans:"+uidStr)
	calls = tg.wait(t, n, "sendMessage")
	send, _ = find(calls, "sendMessage")
	unban := "ab:cf:unban:" + uidStr + ":" + strconv.FormatInt(bans[0].ID, 10)
	found = false
	for _, data := range buttons(send) {
		if data == unban {
			found = true
		}
	}
	if !found {
		t.Fatalf("no unban button in %v", buttons(send))
	}
	n = tg.count()
	userPress(tg, chat, unban)
	calls = tg.wait(t, n, "sendMessage")
	send, _ = find(calls, "sendMessage")
	if body := text(send); !strings.Contains(body, "Разблокировать") {
		t.Fatalf("unban confirm: %q", body)
	}
	n = tg.count()
	userPress(tg, chat, "ab:do:unban:"+uidStr+":"+strconv.FormatInt(bans[0].ID, 10))
	calls = tg.wait(t, n, "sendMessage")
	send, _ = find(calls, "sendMessage")
	if body := text(send); !strings.Contains(body, "разблокировано") {
		t.Fatalf("unbanned: %q", body)
	}
	bans, _ = bot.d.Store.Q.ListDeviceBans(ctx, db.ListDeviceBansParams{UserID: uid, Until: sql.NullInt64{Int64: 1 << 60, Valid: true}})
	if len(bans) != 0 {
		t.Fatalf("bans after the unban: %+v", bans)
	}
}
