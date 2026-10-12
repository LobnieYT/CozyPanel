package tgbot

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cozy/internal/panel/domain"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
	"cozy/internal/panel/store/db"
)

// TestAdminBotLink runs the admin bot against the fake Bot API: the owner
// links by code, a stranger's request needs the owner's tap, and grants gate
// every command.
func TestAdminBotLink(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }

	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := domain.Seed(ctx, st, now); err != nil {
		t.Fatal(err)
	}
	set := settings.New(st.Q)
	for k, v := range map[string]any{KeyAdminEnabled: true, KeyAdminToken: "123:test"} {
		if err := settings.Set(ctx, set, k, v); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{
		Username:     "root",
		PasswordHash: "x",
		CreatedAt:    now.Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE admins SET is_owner = 1 WHERE id = ?`, owner.ID); err != nil {
		t.Fatal(err)
	}
	op, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "op", PasswordHash: "x", CreatedAt: now.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	_ = op
	if _, err := st.DB.ExecContext(ctx, `UPDATE admins SET scopes = '{"stats":"r"}' WHERE username = 'op'`); err != nil {
		t.Fatal(err)
	}

	tg := &fakeTelegram{blocked: map[int64]bool{}}
	srv := httptest.NewServer(tg)
	t.Cleanup(srv.Close)
	pool := domain.NewPool(st, clock)
	bot := NewAdminBot(AdminDeps{Store: st, Settings: set,
		Users:   domain.NewUsers(st, pool, noChanges{}, clock),
		Devices: domain.NewDevices(st, pool, noChanges{}, clock),
		API:     srv.URL, Now: clock, MiniApp: func() bool { return true },
		AdminBase: func(context.Context) string { return "https://panel.example/x" },
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)), Limits: fast})
	go bot.Run(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for !bot.AdminStatus().Running {
		if time.Now().After(deadline) {
			t.Fatalf("the admin bot did not start: %+v", bot.AdminStatus())
		}
		time.Sleep(10 * time.Millisecond)
	}

	const ownerChat, opChat = 111, 222
	var msgID int64
	tell := func(from int64, s string) {
		msgID++
		tg.push(Update{Message: &Message{MessageID: msgID, From: &User{ID: from, FirstName: "A"}, Chat: Chat{ID: from, Type: "private"}, Text: s}})
	}

	// A stranger's command needs a link first.
	n := tg.count()
	tell(opChat, "/stats")
	calls := tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "не привязан") {
		t.Fatalf("unlinked /stats: %q", text(send))
	}

	// The owner links by code from the panel.
	secret, err := bot.secret(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n = tg.count()
	tell(ownerChat, "/start "+AdminLinkCode(secret, owner.ID, now))
	calls = tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "привязан") {
		t.Fatalf("owner link: %q", text(send))
	}
	if _, err := bot.getAdminChat(ctx, ownerChat); err != nil {
		t.Fatalf("owner chat row: %v", err)
	}

	// A code of the subscriber bot never opens the admin one.
	n = tg.count()
	tell(opChat, "/start "+LinkCode(secret, op.ID, now))
	calls = tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "устарела") && !strings.Contains(text(send), "владелец") {
		t.Fatalf("foreign code: %q", text(send))
	}

	// The sub-admin names their login; the owner taps approve.
	n = tg.count()
	tell(opChat, "/start")
	calls = tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "логин") {
		t.Fatalf("ask login: %q", text(send))
	}
	n = tg.count()
	tell(opChat, "op")
	calls = tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "Заявка") {
		t.Fatalf("request filed: %q", text(send))
	}
	// The owner got the approve buttons naming the claimed login.
	var approve string
	tg.until(t, n, func(cs []call) bool {
		for _, c := range cs {
			if c.method != "sendMessage" {
				continue
			}
			for _, data := range buttons(c) {
				if strings.HasPrefix(data, "ab:approve:") {
					approve = data
					return true
				}
			}
		}
		return false
	})
	if approve == "" {
		t.Fatal("no approve button for the owner")
	}
	n = tg.count()
	tg.push(Update{CallbackQuery: &CallbackQuery{ID: "cb1", From: User{ID: ownerChat}, Message: &Message{MessageID: 7, Chat: Chat{ID: ownerChat, Type: "private"}}, Data: approve}})
	calls = tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "Доступ открыт") {
		t.Fatalf("approve: %q", text(send))
	}

	// Grants gate: stats allowed (r), user lookup refused, broadcast owner-only.
	n = tg.count()
	tell(opChat, "/stats")
	calls = tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "Пользователей") {
		t.Fatalf("stats: %q", text(send))
	}
	n = tg.count()
	tell(opChat, "/user x")
	calls = tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "Нет прав") {
		t.Fatalf("user lookup by stats-only: %q", text(send))
	}
	n = tg.count()
	tell(opChat, "/broadcast hi")
	calls = tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "Нет прав") && !strings.Contains(text(send), "владелец") {
		t.Fatalf("broadcast by non-owner: %q", text(send))
	}

	// Unlink drops the chat; commands need a link again.
	n = tg.count()
	tell(opChat, "/unlink")
	calls = tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "отвязан") {
		t.Fatalf("unlink: %q", text(send))
	}
}
