package tgbot

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"cozy/internal/panel/domain"
	"cozy/internal/panel/settings"
)

// hasButton says whether the keyboard carries a row with that callback data.
func hasButton(kb *Keyboard, data string) bool {
	if kb == nil {
		return false
	}
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			if b.CallbackData == data {
				return true
			}
		}
	}
	return false
}

// The subscriber takes a new link from the bot: the button, the confirm, and the wait
// line once the limit is spent. Another chat's tap does nothing.
func TestBotReissuesTheLink(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	old := e.user.SubToken
	// The subscription belongs to this chat first.
	secret, err := e.bot.secret(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e.later()
	n := e.tg.count()
	e.say(555, "/start "+LinkCode(secret, e.user.ID, e.clock()))
	e.tg.wait(t, n, "sendMessage")

	// The button under the subscription screen.
	txt, kb := e.bot.screen(ctx, e.bot.Config(ctx), 555, "s", "")
	if kb == nil || !hasButton(kb, "rl") {
		t.Fatalf("no new-link button: %v", kb)
	}
	// The confirm names the subscription and warns.
	txt, kb, ok := e.bot.reissueConfirm(ctx, wordsFor("ru"), e.user)
	if !ok || !strings.Contains(txt, e.user.Name) || !strings.Contains(txt, "устройства") {
		t.Fatalf("confirm: %q", txt)
	}
	if !hasButton(kb, "rn:"+strconv.FormatInt(e.user.ID, 10)) {
		t.Fatalf("no issue button: %v", kb)
	}
	// Another chat may not act for this one.
	if sc, notice := e.bot.reissueAct(ctx, 999, e.user.ID); sc != "m" || notice != "" {
		t.Fatalf("a foreign chat issued a link: %q %q", sc, notice)
	}
	// The chat's own tap does it.
	sc, notice := e.bot.reissueAct(ctx, 555, e.user.ID)
	if sc != "c" || !strings.Contains(notice, "Новая ссылка готова") {
		t.Fatalf("issued: %q %q", sc, notice)
	}
	fresh, _ := e.st.Q.GetUser(ctx, e.user.ID)
	if fresh.SubToken == old {
		t.Fatal("the token did not change")
	}
	if _, err := e.st.Q.GetUserBySubToken(ctx, old); err == nil {
		t.Fatal("the old token still resolves")
	}
	// The limit is spent: no button, only the wait line.
	e.user = fresh
	btn, line := e.bot.reissueRow(ctx, wordsFor("ru"), e.user)
	if btn != nil || line == "" || !strings.Contains(line, "можно снова") {
		t.Fatalf("after the limit: %v %q", btn, line)
	}
	// A stale tap answers with the same wait and changes nothing.
	before, _ := e.st.Q.GetUser(ctx, e.user.ID)
	if sc, notice := e.bot.reissueAct(ctx, 555, e.user.ID); sc != "s" || !strings.Contains(notice, "можно снова") {
		t.Fatalf("stale tap: %q %q", sc, notice)
	}
	after, _ := e.st.Q.GetUser(ctx, e.user.ID)
	if after.SubToken != before.SubToken {
		t.Fatal("the stale tap issued a link")
	}
	// A day later the button is back.
	e.mu.Lock()
	e.now = e.now.Add(24 * time.Hour)
	e.mu.Unlock()
	btn, line = e.bot.reissueRow(ctx, wordsFor("ru"), e.user)
	if btn == nil || line != "" {
		t.Fatalf("after a day: %v %q", btn, line)
	}
	// Rules off: nothing on the screen, the action refuses.
	if err := settings.Set(ctx, e.set, settings.KeyReissueRules, domain.ReissueRules{Off: true, Limit: 1, Days: 1}); err != nil {
		t.Fatal(err)
	}
	btn, line = e.bot.reissueRow(ctx, wordsFor("ru"), e.user)
	if btn != nil || line != "" {
		t.Fatalf("off: %v %q", btn, line)
	}
	if _, kb, ok := e.bot.reissueConfirm(ctx, wordsFor("ru"), e.user); ok || kb != nil {
		t.Fatal("confirm must refuse when off")
	}
	if sc, notice := e.bot.reissueAct(ctx, 555, e.user.ID); sc != "s" || !strings.Contains(notice, "администратор") {
		t.Fatalf("off action: %q %q", sc, notice)
	}
}
