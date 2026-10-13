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

func TestAdminMenuValidate(t *testing.T) {
	m := DefaultAdminMenu("ru")
	if err := m.Validate(); err != nil {
		t.Fatalf("default menu: %v", err)
	}
	if len(m.Buttons) != 7 {
		t.Fatalf("default buttons: %d", len(m.Buttons))
	}
	bad := DefaultAdminMenu("ru")
	bad.Buttons[0].Label = ""
	if err := bad.Validate(); err == nil {
		t.Fatal("empty label passes")
	}
	bad = DefaultAdminMenu("ru")
	bad.Buttons[0].Action = "nope"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown action passes")
	}
	bad = DefaultAdminMenu("ru")
	bad.Buttons[1].Action = AdminActStats
	if err := bad.Validate(); err == nil {
		t.Fatal("duplicate builtin passes")
	}
	bad = DefaultAdminMenu("ru")
	bad.Texts.Help = "hi <имя>"
	if err := bad.Validate(); err == nil {
		t.Fatal("angle brackets in texts pass")
	}
	m = DefaultAdminMenu("xx")
	if err := m.Validate(); err != nil || m.Lang != "ru" {
		t.Fatalf("lang normalize: %v %q", err, m.Lang)
	}
}

// menuEnv starts an admin bot with a linked owner chat for menu tests.
func menuEnv(t *testing.T) (*AdminBot, *fakeTelegram, int64, context.Context) {
	t.Helper()
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
	owner, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "root", PasswordHash: "x", CreatedAt: now.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE admins SET is_owner = 1 WHERE id = ?`, owner.ID); err != nil {
		t.Fatal(err)
	}
	_ = owner
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
	const chat = 111
	var msgID int64
	_ = msgID
	if err := bot.upsertAdminChat(ctx, chat, 1, "root", "Root", now.Unix()); err != nil {
		t.Fatal(err)
	}
	return bot, tg, chat, ctx
}

func menuSay(tg *fakeTelegram, msgID *int64, from int64, s string) {
	*msgID++
	tg.push(Update{Message: &Message{MessageID: *msgID, From: &User{ID: from, FirstName: "R"}, Chat: Chat{ID: from, Type: "private"}, Text: s}})
}

func menuPress(tg *fakeTelegram, from int64, data string) {
	tg.push(Update{CallbackQuery: &CallbackQuery{ID: "cb-" + data, From: User{ID: from}, Message: &Message{MessageID: 9, Chat: Chat{ID: from, Type: "private"}}, Data: data}})
}

// /help must arrive: the literal <> placeholders once silenced it for good.
func TestAdminMenuHelpArrives(t *testing.T) {
	_, tg, chat, _ := menuEnv(t)
	var msgID int64
	n := tg.count()
	menuSay(tg, &msgID, chat, "/help")
	calls := tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "/stats") {
		t.Fatalf("help: %q", text(send))
	}
}

// /menu shows configured buttons; an off button disappears.
func TestAdminMenuButtons(t *testing.T) {
	bot, tg, chat, ctx := menuEnv(t)
	var msgID int64
	n := tg.count()
	menuSay(tg, &msgID, chat, "/menu")
	calls := tg.wait(t, n, "sendMessage")
	send, _ := find(calls, "sendMessage")
	btns := buttons(send)
	if btns["📊 Статистика"] != "ab:m:stats" {
		t.Fatalf("menu buttons: %v", btns)
	}
	m := DefaultAdminMenu("ru")
	for i := range m.Buttons {
		if m.Buttons[i].Action == AdminActStats {
			m.Buttons[i].On = false
		}
	}
	if err := settings.Set(ctx, bot.d.Settings, KeyAdminMenu, m); err != nil {
		t.Fatal(err)
	}
	n = tg.count()
	menuSay(tg, &msgID, chat, "/menu")
	calls = tg.wait(t, n, "sendMessage")
	send, _ = find(calls, "sendMessage")
	for _, data := range buttons(send) {
		if data == "ab:m:stats" {
			t.Fatalf("disabled button shown: %v", buttons(send))
		}
	}
}

// Two-step find: button, then free text with the query.
func TestAdminMenuFindFlow(t *testing.T) {
	bot, tg, chat, ctx := menuEnv(t)
	pool := domain.NewPool(bot.d.Store, bot.d.Now)
	users := domain.NewUsers(bot.d.Store, pool, noChanges{}, bot.d.Now)
	tariffs, _ := bot.d.Store.Q.ListTariffs(ctx)
	u, err := users.Create(ctx, domain.CreateInput{Name: "Тест", TariffID: tariffs[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = u
	var msgID int64
	menuPress(tg, chat, "ab:m:find")
	n := tg.count()
	calls := tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "Кого") {
		t.Fatalf("find ask: %q", text(send))
	}
	n = tg.count()
	menuSay(tg, &msgID, chat, "Тест")
	calls = tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "Тест") {
		t.Fatalf("find result: %q", text(send))
	}
}

// Two-step broadcast: button, text, confirm, sends.
func TestAdminMenuBroadcastFlow(t *testing.T) {
	_, tg, chat, _ := menuEnv(t)
	menuPress(tg, chat, "ab:m:broadcast")
	n := tg.count()
	calls := tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "Текст") {
		t.Fatalf("broadcast ask: %q", text(send))
	}
	var msgID int64
	n = tg.count()
	menuSay(tg, &msgID, chat, "hello all")
	tg.until(t, n, func(cs []call) bool {
		for _, c := range cs {
			for _, data := range buttons(c) {
				if data == "ab:bc:go" {
					return true
				}
			}
		}
		return false
	})
	n = tg.count()
	menuPress(tg, chat, "ab:bc:go")
	calls = tg.wait(t, n, "sendMessage")
	queued := false
	for _, c := range calls {
		if c.method == "sendMessage" && strings.Contains(text(c), "В очереди") {
			queued = true
		}
	}
	if !queued {
		t.Fatal("broadcast never queued")
	}
}

// Configured help text replaces the built-in one.
func TestAdminMenuHelpOverride(t *testing.T) {
	bot, tg, chat, ctx := menuEnv(t)
	m := DefaultAdminMenu("ru")
	m.Texts.Help = "Своя справка."
	if err := settings.Set(ctx, bot.d.Settings, KeyAdminMenu, m); err != nil {
		t.Fatal(err)
	}
	var msgID int64
	n := tg.count()
	menuSay(tg, &msgID, chat, "/help")
	calls := tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "Своя справка.") {
		t.Fatalf("help override: %q", text(send))
	}
}
