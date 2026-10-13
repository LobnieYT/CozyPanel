package tgbot

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"cozy/internal/panel/domain"
)

// usersEnv starts a menu bot with one panel user to manage.
func usersEnv(t *testing.T) (*AdminBot, *fakeTelegram, int64, int64) {
	t.Helper()
	bot, tg, chat, ctx := menuEnv(t)
	pool := domain.NewPool(bot.d.Store, bot.d.Now)
	users := domain.NewUsers(bot.d.Store, pool, noChanges{}, bot.d.Now)
	tariffs, err := bot.d.Store.Q.ListTariffs(ctx)
	if err != nil || len(tariffs) == 0 {
		t.Fatalf("tariffs: %v", tariffs)
	}
	u, err := users.Create(ctx, domain.CreateInput{Name: "Клиент", Contact: "@k", TariffID: tariffs[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	bot.d.DataDir = t.TempDir()
	return bot, tg, chat, u.ID
}

func userTell(tg *fakeTelegram, msgID *int64, chat int64, s string) {
	*msgID++
	tg.push(Update{Message: &Message{MessageID: *msgID, From: &User{ID: chat, FirstName: "R"}, Chat: Chat{ID: chat, Type: "private"}, Text: s}})
}

func userPress(tg *fakeTelegram, chat int64, data string) {
	tg.push(Update{CallbackQuery: &CallbackQuery{ID: "cb-" + data, From: User{ID: chat}, Message: &Message{MessageID: 9, Chat: Chat{ID: chat, Type: "private"}}, Data: data}})
}

func waitText(t *testing.T, tg *fakeTelegram, n int) string {
	t.Helper()
	calls := tg.wait(t, n, "sendMessage")
	send, _ := find(calls, "sendMessage")
	return text(send)
}

func TestAdminUsersList(t *testing.T) {
	_, tg, chat, uid := usersEnv(t)
	var msgID int64
	n := tg.count()
	userTell(tg, &msgID, chat, "/users")
	calls := tg.wait(t, n, "sendMessage")
	send, _ := find(calls, "sendMessage")
	if body := text(send); !strings.Contains(body, "Все (1/1)") {
		t.Fatalf("list header: %q", body)
	}
	for _, data := range buttons(send) {
		if data == "ab:user:"+strconv.FormatInt(uid, 10) {
			return
		}
	}
	t.Fatalf("no user button in %v", buttons(send))
}

func TestAdminUsersCard(t *testing.T) {
	_, tg, chat, uid := usersEnv(t)
	userPress(tg, chat, "ab:user:"+strconv.FormatInt(uid, 10))
	n := tg.count()
	_ = n
	calls := tg.wait(t, 0, "sendMessage")
	var card string
	for _, c := range calls {
		if c.method == "sendMessage" && strings.Contains(text(c), "Клиент") {
			card = text(c)
		}
	}
	_ = card
	n = tg.count()
	userPress(tg, chat, "ab:ux:m:"+strconv.FormatInt(uid, 10))
	calls = tg.wait(t, n, "sendMessage")
	send, _ := find(calls, "sendMessage")
	btns := buttons(send)
	for _, want := range []string{"ab:cf:delete:" + strconv.FormatInt(uid, 10), "ab:cf:reissue:" + strconv.FormatInt(uid, 10), "ab:ux:k:" + strconv.FormatInt(uid, 10)} {
		found := false
		for _, data := range btns {
			if data == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("more view misses %s in %v", want, btns)
		}
	}
}

func TestAdminUserCreate(t *testing.T) {
	bot, tg, chat, _ := usersEnv(t)
	var msgID int64
	n := tg.count()
	userTell(tg, &msgID, chat, "/new")
	if got := waitText(t, tg, n); !strings.Contains(got, "Имя") {
		t.Fatalf("ask name: %q", got)
	}
	n = tg.count()
	userTell(tg, &msgID, chat, "Новичок")
	if got := waitText(t, tg, n); !strings.Contains(got, "Контакт") {
		t.Fatalf("ask contact: %q", got)
	}
	n = tg.count()
	userTell(tg, &msgID, chat, "-")
	calls := tg.wait(t, n, "sendMessage")
	send, _ := find(calls, "sendMessage")
	var pick string
	for _, data := range buttons(send) {
		if strings.HasPrefix(data, "ab:xn:") {
			pick = data
		}
	}
	if pick == "" {
		t.Fatalf("no tariff buttons in %q", text(send))
	}
	n = tg.count()
	userPress(tg, chat, pick)
	calls = tg.wait(t, n, "sendMessage")
	ok := false
	for _, c := range calls {
		if c.method == "sendMessage" && strings.Contains(text(c), "Создан") {
			ok = true
		}
	}
	if !ok {
		t.Fatal("user not created")
	}
	users, _ := bot.d.Store.Q.ListUsers(context.Background())
	found := false
	for _, u := range users {
		if u.Name == "Новичок" {
			found = true
		}
	}
	if !found {
		t.Fatal("created user missing in db")
	}
}

func TestAdminUserExtendDelete(t *testing.T) {
	bot, tg, chat, uid := usersEnv(t)
	id := strconv.FormatInt(uid, 10)
	userPress(tg, chat, "ab:x:"+id+":30")
	n := tg.count()
	calls := tg.wait(t, n, "sendMessage")
	if send, _ := find(calls, "sendMessage"); !strings.Contains(text(send), "Продлён") {
		t.Fatalf("extend: %q", text(send))
	}
	userPress(tg, chat, "ab:cf:delete:"+id)
	n = tg.count()
	calls = tg.wait(t, n, "sendMessage")
	var confirm string
	for _, c := range calls {
		for _, data := range buttons(c) {
			if strings.HasPrefix(data, "ab:do:delete:") {
				confirm = data
			}
		}
	}
	if confirm == "" {
		t.Fatal("no delete confirm")
	}
	n = tg.count()
	userPress(tg, chat, confirm)
	tg.wait(t, n, "sendMessage")
	if _, err := bot.d.Users.Get(context.Background(), uid); err == nil {
		t.Fatal("user survives delete")
	}
}

func TestAdminKeysWarn(t *testing.T) {
	_, tg, chat, uid := usersEnv(t)
	id := strconv.FormatInt(uid, 10)
	n := tg.count()
	userPress(tg, chat, "ab:ux:k:"+id)
	calls := tg.wait(t, n, "sendMessage")
	send, _ := find(calls, "sendMessage")
	body := text(send)
	if strings.Contains(body, "vless://") || strings.Contains(body, "若http") {
		t.Fatalf("secrets leak before tap: %q", body)
	}
}
