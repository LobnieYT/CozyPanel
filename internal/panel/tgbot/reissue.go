package tgbot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strconv"
	"time"

	"cozy/internal/panel/domain"
	"cozy/internal/panel/store/db"
)

// The subscriber's own new link: a button under the subscription screen, a confirm, and
// the rules the admin set (off, one a day by default).
type linkWords struct {
	button, confirm, yes, done, off, failed string
	wait                                    string
}

var linkRu = &linkWords{
	button:  "🔄 Новая ссылка",
	confirm: "Выпустить новую ссылку для «%s»?\n\nСтарая ссылка перестанет работать сразу, а все привязанные устройства отключатся. Приложения придётся подписать заново: добавьте в них новую ссылку.",
	yes:     "✅ Да, выпустить",
	done:    "✅ Новая ссылка готова. Старая больше не работает: добавьте новую в приложения.",
	off:     "Сменить ссылку может только администратор: напишите в поддержку.",
	failed:  "⚠️ Не получилось выпустить новую ссылку. Попробуйте ещё раз чуть позже.",
	wait:    "🔄 Новая ссылка: можно снова через %s",
}

var linkEn = &linkWords{
	button:  "🔄 New link",
	confirm: "Issue a new link for “%s”?\n\nThe old link stops working at once and all bound devices are disconnected. The apps need the new link: add it to each of them.",
	yes:     "✅ Yes, issue it",
	done:    "✅ The new link is ready. The old one no longer works: add the new one to your apps.",
	off:     "Only the administrator can change the link: write to support.",
	failed:  "⚠️ The new link was not issued. Please try again a little later.",
	wait:    "🔄 New link: available again in %s",
}

func linkWordsFor(w *words) *linkWords {
	if w == &en {
		return linkEn
	}
	return linkRu
}

// in names when the next link frees up, to the hour.
func (b *Bot) in(w *words, t time.Time) string {
	hours := int(t.Sub(b.d.Now()).Hours() + 0.5)
	return w.span(max(hours, 1))
}

func (b *Bot) reissueState(ctx context.Context, u db.User) (domain.ReissueState, bool) {
	if b.d.Users == nil {
		return domain.ReissueState{}, false
	}
	st, err := b.d.Users.ReissueState(ctx, u.ID)
	if err != nil {
		b.d.Log.Warn("telegram: reissue rules", "err", err)
		return domain.ReissueState{}, false
	}
	return st, true
}

// reissueRow is the button under the subscription screen and the line under it: nothing
// when rules are unreadable or off, a wait line when the limit is spent.
func (b *Bot) reissueRow(ctx context.Context, w *words, u db.User) (*Button, string) {
	lw := linkWordsFor(w)
	st, ok := b.reissueState(ctx, u)
	if !ok || st.Rules.Off {
		return nil, ""
	}
	if !st.Next.IsZero() {
		return nil, html.EscapeString(fmt.Sprintf(lw.wait, b.in(w, st.Next)))
	}
	return &Button{Text: lw.button, CallbackData: "rl"}, ""
}

// reissueConfirm asks before the link changes; a spent limit or rules off answers with
// the reason instead of the buttons.
func (b *Bot) reissueConfirm(ctx context.Context, w *words, u db.User) (string, *Keyboard, bool) {
	lw := linkWordsFor(w)
	st, ok := b.reissueState(ctx, u)
	if !ok {
		return "", nil, false
	}
	if st.Rules.Off {
		return lw.off, nil, false
	}
	if !st.Next.IsZero() {
		return fmt.Sprintf(lw.wait, b.in(w, st.Next)), nil, false
	}
	return fmt.Sprintf(lw.confirm, html.EscapeString(u.Name)), &Keyboard{[][]Button{
		{{Text: lw.yes, CallbackData: "rn:" + strconv.FormatInt(u.ID, 10)}, {Text: w.cancel, CallbackData: "s"}}}}, true
}

// reissueAct issues the new link and says what happened; a chat may only do its own.
func (b *Bot) reissueAct(ctx context.Context, chat, id int64) (screen, notice string) {
	if b.d.Users == nil {
		return "m", ""
	}
	w := wordsFor(b.lang(ctx))
	lw := linkWordsFor(w)
	_, u, ok := b.subs(ctx, chat)
	if !ok || u.ID != id {
		return "m", ""
	}
	_, err := b.d.Users.ReissueByOwner(ctx, id, "bot")
	switch {
	case err == nil:
		_ = b.d.Store.Q.SetTgCurrent(ctx, db.SetTgCurrentParams{Current: id, TgID: chat})
		return "c", lw.done
	case errors.Is(err, domain.ErrReissueCooldown):
		st, _ := b.reissueState(ctx, u)
		return "s", fmt.Sprintf(lw.wait, b.in(w, st.Next))
	case errors.Is(err, domain.ErrReissueOff):
		return "s", lw.off
	case errors.Is(err, domain.ErrNotFound):
		return "m", ""
	default:
		b.d.Log.Warn("telegram: link not reissued", "err", err)
		return "s", lw.failed
	}
}
