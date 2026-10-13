package tgbot

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"cozy/internal/panel/audit"
	"cozy/internal/panel/domain"
	"cozy/internal/panel/store/db"
)

// Bot commands of the admin bot. Every command resolves the tapping admin
// fresh (a term or a switch-off stops them at once) and checks the matrix:
// the owner passes everything, managing admins stays owner-only like the
// admins section of the panel.

// adminWords is the bot's texts; the panel language picks one.
type adminWords struct {
	commands, menuApp                 string
	help, helpOwner                   string
	needLink, needOwner, linked       string
	askLogin                          string
	menuHead, askQuery, askBcast      string
	noRequests                        string
	noAccess, closed                  string
	requestSent, requestApproved      string
	requestRefused, requestGone       string
	approveAsk                        string
	approved, refused                 string
	stats                             string
	userTitle, userNotFound, userMany string
	confirmAsk, cancelled             string
	actDisabled, actEnabled           string
	actExtended, actReset, actUnbound string
	unbindPick                        string
	nodesTitle, noNodes               string
	broadcastAsk, broadcastSent       string
	broadcastUsage                    string
	unlinked                          string
	noBound                           string
	disableVerb, enableVerb           string
	extendVerb, resetVerb, unbindVerb string
	deleteVerb, reissueVerb           string
	tariffVerb, tgunlinkVerb          string
	foreverVerb                       string
	unknown                           string
	usersTitle                        string
	trToday, trWeek                   string
	fAll, fActive, fExpiring          string
	fLimited, fExpired, fDisabled     string
	back                              string
	moreActions                       string
	newAskName, newAskContact         string
	newAskTariff, userCreated         string
	dateAsk, dateBad                  string
	billAsk, billBad                  string
	linkWarn, keysWarn                string
	linkTitle, keysTitle              string
	noKeys, noTg                      string
	tariffPick, limitPick             string
	expPick, inbPick                  string
	inbEmpty, avatarNone              string
	poolsTitle, grantsTitle           string
	noteAsk, contactAsk               string
	applied                           string
}

func adminText(lang string) adminWords {
	if lang != "en" {
		return adminWords{
			commands:        "Панель",
			menuApp:         "Панель",
			help:            "Команды:\n/stats — сводка\n/users — список\n/user «имя» — найти подписку\n/new — новый пользователь\n/nodes — ноды\n/unlink — отвязать этот чат",
			helpOwner:       "\n/broadcast «текст» — всем привязанным админам",
			needLink:        "Этот чат не привязан. Отправьте /start — владелец подтвердит заявку.",
			askLogin:        "Пришлите ваш логин администратора — владелец подтвердит заявку.",
			menuHead:        "Панель Cozy — выбери действие.",
			askQuery:        "Кого ищем? Пришли имя, контакт или ID.",
			askBcast:        "Текст рассылки?",
			noRequests:      "Заявок нет.",
			needOwner:       "Нужен владелец.",
			linked:          "Чат привязан. /help — команды.",
			noAccess:        "Нет прав на этот раздел.",
			closed:          "Аккаунт отключён или срок вышел.",
			requestSent:     "Заявка отправлена владельцу. Как подтвердит — напишу.",
			requestApproved: "Доступ открыт. /help — команды.",
			requestRefused:  "Владелец отклонил заявку.",
			requestGone:     "Заявка устарела.",
			approveAsk:      "Просит доступ:",
			approved:        "Доступ открыт.",
			refused:         "Заявка отклонена.",
			stats:           "👥 Пользователей: {users}\n🟢 В сети: {online}\n🖥 Нод: {nodes}",
			userTitle:       "#{id} {name}",
			userNotFound:    "Никого не нашёл.",
			userMany:        "…и ещё {n}.",
			confirmAsk:      "{what} «{name}»?",
			cancelled:       "Отменено.",
			actDisabled:     "Выключен.",
			actEnabled:      "Включён.",
			actExtended:     "Продлён.",
			actReset:        "Трафик сброшен.",
			actUnbound:      "Устройство отвязано.",
			unbindPick:      "Какое отвязать?",
			nodesTitle:      "Ноды:",
			noNodes:         "Нод нет.",
			broadcastAsk:    "Разослать {n} админам?\n\n{text}",
			broadcastSent:   "В очереди: {n}.",
			broadcastUsage:  "Текст после команды: /broadcast Текст.",
			unlinked:        "Чат отвязан.",
			noBound:         "Привязанных устройств нет.",
			disableVerb:     "Выключить",
			enableVerb:      "Включить",
			extendVerb:      "Продлить на 30 дней",
			resetVerb:       "Сбросить трафик",
			unbindVerb:      "Отвязать устройство",
			deleteVerb:      "Удалить",
			foreverVerb:     "Бессрочно",
			reissueVerb:     "Перевыпустить",
			tariffVerb:      "Сменить тариф",
			tgunlinkVerb:    "Отвязать TG",
			unknown:         "Не понял. /help — команды.",
			usersTitle:      "Пользователи",
			trToday:         "Сегодня: ↑{up} ↓{down}",
			trWeek:          "7 дней: ↑{up} ↓{down} {bars}",
			fAll:            "Все",
			fActive:         "Активные",
			fExpiring:       "Истекают",
			fLimited:        "Лимит",
			fExpired:        "Истекли",
			fDisabled:       "Выкл",
			back:            "« Назад",
			moreActions:     "Ещё действия",
			newAskName:      "Имя нового пользователя?",
			newAskContact:   "Контакт? Пришли текст или «-», чтобы пропустить.",
			newAskTariff:    "Тариф?",
			userCreated:     "Создан.",
			dateAsk:         "Дата в виде ДД.ММ.ГГГГ?",
			dateBad:         "Не понял дату. Нужно ДД.ММ.ГГГГ.",
			billAsk:         "День оплаты 1–31? Пришли 0, чтобы убрать.",
			billBad:         "Нужно число 0–31.",
			linkWarn:        "⚠️ Ссылка останется в истории чата. Показать?",
			keysWarn:        "⚠️ Ключи останутся в истории чата. Показать?",
			linkTitle:       "Ссылка подписки",
			keysTitle:       "Ключи",
			noKeys:          "Нет подключений со ссылками.",
			noTg:            "Telegram не привязан.",
			tariffPick:      "Тариф?",
			limitPick:       "Лимит устройств?",
			expPick:         "На сколько продлить?",
			inbPick:         "Подключения (тап — вкл/выкл):",
			inbEmpty:        "Нельзя выключить все.",
			avatarNone:      "Аватарки нет.",
			poolsTitle:      "Пулы и пакеты",
			grantsTitle:     "Пакеты",
			noteAsk:         "Текст заметки?",
			contactAsk:      "Новый контакт?",
			applied:         "Готово.",
		}
	}
	return adminWords{
		commands:        "Panel",
		menuApp:         "Panel",
		help:            "Commands:\n/stats — summary\n/users — list\n/user \"name\" — find a subscription\n/new — new user\n/nodes — nodes\n/unlink — unlink this chat",
		helpOwner:       "\n/broadcast \"text\" — to every linked admin",
		needLink:        "This chat is not linked. Send /start — the owner approves requests.",
		askLogin:        "Send your admin login — the owner approves requests.",
		menuHead:        "Cozy panel — pick an action.",
		askQuery:        "Who? Send a name, contact or ID.",
		askBcast:        "Broadcast text?",
		noRequests:      "No requests.",
		needOwner:       "The owner is needed.",
		linked:          "Chat linked. /help for commands.",
		noAccess:        "No access to this section.",
		closed:          "The account is disabled or expired.",
		requestSent:     "Request sent to the owner. I will write back on approval.",
		requestApproved: "Access granted. /help for commands.",
		requestRefused:  "The owner refused the request.",
		requestGone:     "The request expired.",
		approveAsk:      "Asks for access:",
		approved:        "Access granted.",
		refused:         "Request refused.",
		stats:           "👥 Users: {users}\n🟢 Online: {online}\n🖥 Nodes: {nodes}",
		userTitle:       "#{id} {name}",
		userNotFound:    "Nobody found.",
		userMany:        "…and {n} more.",
		confirmAsk:      "{what} “{name}”?",
		cancelled:       "Cancelled.",
		actDisabled:     "Disabled.",
		actEnabled:      "Enabled.",
		actExtended:     "Extended.",
		actReset:        "Traffic reset.",
		actUnbound:      "Device unbound.",
		unbindPick:      "Which one to unbind?",
		nodesTitle:      "Nodes:",
		noNodes:         "No nodes.",
		broadcastAsk:    "Send to {n} admins?\n\n{text}",
		broadcastSent:   "Queued: {n}.",
		broadcastUsage:  "Text after the command: /broadcast Text.",
		unlinked:        "Chat unlinked.",
		noBound:         "No bound devices.",
		disableVerb:     "Disable",
		enableVerb:      "Enable",
		extendVerb:      "Extend 30 days",
		resetVerb:       "Reset traffic",
		unbindVerb:      "Unbind device",
		deleteVerb:      "Delete",
		foreverVerb:     "Forever",
		reissueVerb:     "Reissue",
		tariffVerb:      "Change tariff",
		tgunlinkVerb:    "Unlink TG",
		unknown:         "Did not get it. /help for commands.",
		usersTitle:      "Users",
		trToday:         "Today: ↑{up} ↓{down}",
		trWeek:          "7 days: ↑{up} ↓{down} {bars}",
		fAll:            "All",
		fActive:         "Active",
		fExpiring:       "Expiring",
		fLimited:        "Limited",
		fExpired:        "Expired",
		fDisabled:       "Off",
		back:            "« Back",
		moreActions:     "More actions",
		newAskName:      "New user name?",
		newAskContact:   "Contact? Send text or “-” to skip.",
		newAskTariff:    "Tariff?",
		userCreated:     "Created.",
		dateAsk:         "Date as DD.MM.YYYY?",
		dateBad:         "Did not get the date. Need DD.MM.YYYY.",
		billAsk:         "Billing day 1–31? Send 0 to clear.",
		billBad:         "Need a number 0–31.",
		linkWarn:        "⚠️ The link stays in chat history. Show?",
		keysWarn:        "⚠️ Keys stay in chat history. Show?",
		linkTitle:       "Subscription link",
		keysTitle:       "Keys",
		noKeys:          "No linkable connections.",
		noTg:            "Telegram not linked.",
		tariffPick:      "Tariff?",
		limitPick:       "Device limit?",
		expPick:         "Extend for?",
		inbPick:         "Connections (tap toggles):",
		inbEmpty:        "Cannot switch them all off.",
		avatarNone:      "No avatar.",
		poolsTitle:      "Pools and packages",
		grantsTitle:     "Packages",
		noteAsk:         "Note text?",
		contactAsk:      "New contact?",
		applied:         "Done.",
	}
}

func (b *AdminBot) words() adminWords { return adminText("ru") }

// langWords renders with the panel language.
func (b *AdminBot) langWords(ctx context.Context) adminWords {
	lang, _ := b.d.Settings.Lang(ctx)
	return adminText(lang)
}

// identity loads the tapping admin fresh: disabled and expired stop here.
func (b *AdminBot) identity(ctx context.Context, tgID int64) (db.Admin, error) {
	chat, err := b.getAdminChat(ctx, tgID)
	if err != nil {
		return db.Admin{}, errAdminNotFound
	}
	admin, err := b.d.Store.Q.GetAdmin(ctx, chat.AdminID)
	if err != nil {
		return db.Admin{}, errAdminNotFound
	}
	if admin.DisabledAt.Valid {
		return db.Admin{}, errAdminClosed
	}
	if admin.ExpiresAt.Valid && !time.Unix(admin.ExpiresAt.Int64, 0).After(b.d.Now()) {
		return db.Admin{}, errAdminClosed
	}
	return admin, nil
}

func (b *AdminBot) say(out *Outbox, chat int64, text string, kb *Keyboard) {
	out.Reply(chat, "", 1, func(ctx context.Context, c *Client) error {
		_, err := c.Send(ctx, chat, text, kb, false)
		return err
	})
}

func (b *AdminBot) deny(out *Outbox, chat int64, w adminWords, err error) {
	switch {
	case errors.Is(err, errAdminClosed):
		b.say(out, chat, w.closed, nil)
	default:
		b.say(out, chat, w.noAccess, nil)
	}
}

// handle takes one update of the admin bot: private chats only.
func (b *AdminBot) handle(ctx context.Context, c *Client, up Update) error {
	out := b.out.Load()
	if out == nil {
		return nil
	}
	switch {
	case up.CallbackQuery != nil && up.CallbackQuery.Message != nil && up.CallbackQuery.Message.Chat.Type == "private":
		b.onAdminPress(ctx, c, out, up.CallbackQuery)
	case up.Message != nil && up.Message.From != nil && up.Message.Chat.Type == "private":
		b.onAdminMessage(ctx, out, up.Message)
	}
	return nil
}

func (b *AdminBot) onAdminMessage(ctx context.Context, out *Outbox, m *Message) {
	chat := m.Chat.ID
	if b.flooding(chat) {
		return
	}
	w := b.langWords(ctx)
	text := strings.TrimSpace(m.Text)
	name := strings.ToLower(text)
	switch {
	case text == "/start" || strings.HasPrefix(text, "/start "):
		b.cmdStart(ctx, out, w, chat, m.From, strings.TrimSpace(strings.TrimPrefix(text, "/start")))
		return
	case name == "/help":
		b.cmdHelp(ctx, out, w, chat)
		return
	case name == "/menu":
		if admin, err := b.identity(ctx, chat); err != nil {
			b.say(out, chat, w.needLink, nil)
		} else {
			b.sendMenu(ctx, out, chat, admin)
		}
		return
	}
	if what := b.takeAwait(chat, strings.HasPrefix(text, "/")); what != "" {
		admin, err := b.identity(ctx, chat)
		if err != nil {
			b.say(out, chat, w.needLink, nil)
			return
		}
		kind, arg, _ := strings.Cut(what, ":")
		switch kind {
		case "find":
			b.cmdUser(ctx, out, w, chat, admin, text)
		case "broadcast":
			b.cmdBroadcast(ctx, out, w, chat, admin, text)
		case "note", "contact", "expdate", "billday":
			b.consumeAwait(ctx, out, w, chat, admin, kind, arg, text)
		case "newname":
			b.consumeAwait(ctx, out, w, chat, admin, kind, "", text)
		case "newcontact":
			b.consumeAwait(ctx, out, w, chat, admin, kind, "", text)
		default:
			b.cmdHelp(ctx, out, w, chat)
		}
		return
	}
	admin, err := b.identity(ctx, chat)
	if err != nil {
		// Unlinked chats claim their admin login with any plain text; commands
		// need a link first.
		if strings.HasPrefix(text, "/") {
			b.say(out, chat, w.needLink, nil)
			return
		}
		b.claimLink(ctx, out, w, chat, m.From, text)
		return
	}
	switch {
	case name == "/stats":
		b.cmdStats(ctx, out, w, chat, admin)
	case strings.HasPrefix(text, "/user ") || text == "/user":
		b.cmdUser(ctx, out, w, chat, admin, strings.TrimSpace(strings.TrimPrefix(text, "/user")))
	case name == "/nodes":
		b.cmdNodes(ctx, out, w, chat, admin)
	case strings.HasPrefix(text, "/broadcast ") || text == "/broadcast":
		b.cmdBroadcast(ctx, out, w, chat, admin, strings.TrimSpace(strings.TrimPrefix(text, "/broadcast")))
	case name == "/unlink":
		b.cmdUnlink(ctx, out, w, chat, admin)
	case name == "/users":
		b.cmdUsers(ctx, out, w, chat, admin, "all", 0)
	case name == "/new":
		if !b.check(out, w, chat, admin, "users", true) {
			return
		}
		b.awaitAsk(chat, "newname")
		b.say(out, chat, w.newAskName, nil)
	default:
		b.cmdHelp(ctx, out, w, chat)
	}
}

func (b *AdminBot) cmdHelp(ctx context.Context, out *Outbox, w adminWords, chat int64) {
	admin, err := b.identity(ctx, chat)
	if err != nil {
		b.say(out, chat, w.needLink, nil)
		return
	}
	b.say(out, chat, menuHelpText(admin, b.AdminMenuConfig(ctx), w), nil)
}

// --- linking ---

func (b *AdminBot) cmdStart(ctx context.Context, out *Outbox, w adminWords, chat int64, from *User, code string) {
	now := b.d.Now()
	if code != "" {
		secret, err := b.secret(ctx)
		if err != nil {
			return
		}
		adminID, err := ParseAdminLinkCode(secret, code, now)
		if err != nil {
			b.say(out, chat, w.requestGone, nil)
			return
		}
		admin, err := b.d.Store.Q.GetAdmin(ctx, adminID)
		if err != nil || admin.IsOwner == 0 {
			b.say(out, chat, w.needOwner, nil)
			return
		}
		if err := b.linkChat(ctx, admin, chat, from, now); err != nil {
			return
		}
		b.safeAudit(ctx, admin.ID, "adminbot.link", "admin", admin.Username, map[string]any{"by": "code"})
		b.say(out, chat, w.linked, nil)
		return
	}
	if admin, err := b.identity(ctx, chat); err == nil {
		b.sendMenu(ctx, out, chat, admin)
		return
	}
	// Unlinked /start states the next step; any other text names the claimed login.
	b.say(out, chat, w.askLogin, nil)
}

// claimLink files a link request: the chat names the admin login it claims, the
// owner approves a human they recognize. Garbage claims die on refuse.
func (b *AdminBot) claimLink(ctx context.Context, out *Outbox, w adminWords, chat int64, from *User, login string) {
	now := b.d.Now()
	login = strings.TrimPrefix(strings.TrimSpace(login), "@")
	if r := []rune(login); len(r) == 0 || len(r) > 64 {
		b.say(out, chat, w.askLogin, nil)
		return
	}
	b.mu.Lock()
	b.requests[chat] = adminRequest{username: from.Username, firstName: from.FirstName, login: login, at: now}
	b.mu.Unlock()
	b.pruneRequests(now)
	b.say(out, chat, w.requestSent, nil)
	owner := b.ownerChat(ctx)
	if owner == 0 {
		return
	}
	kb := &Keyboard{InlineKeyboard: [][]Button{
		{{Text: "✅", CallbackData: "ab:approve:" + strconv.FormatInt(chat, 10)},
			{Text: "🚫", CallbackData: "ab:refuse:" + strconv.FormatInt(chat, 10)}},
	}}
	who := "@" + from.Username
	if from.Username == "" {
		who = from.FirstName
		if who == "" {
			who = strconv.FormatInt(chat, 10)
		}
	}
	b.say(out, owner, w.approveAsk+" <b>"+esc(who)+"</b> → <b>"+esc(login)+"</b>", kb)
}

// linkChat binds the chat to the admin, replacing their previous chat.
func (b *AdminBot) linkChat(ctx context.Context, admin db.Admin, chat int64, from *User, now time.Time) error {
	username, first := "", ""
	if from != nil {
		username, first = from.Username, from.FirstName
	}
	if err := b.dropAdminChatsOf(ctx, admin.ID); err != nil {
		return err
	}
	return b.upsertAdminChat(ctx, chat, admin.ID, username, first, now.Unix())
}

func (b *AdminBot) pruneRequests(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, r := range b.requests {
		if now.Sub(r.at) > requestTTL {
			delete(b.requests, id)
		}
	}
}

// pendingRequests lists fresh link requests, oldest first.
func (b *AdminBot) pendingRequests(now time.Time) []adminRequestRow {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []adminRequestRow
	for id, r := range b.requests {
		if now.Sub(r.at) > requestTTL {
			delete(b.requests, id)
			continue
		}
		out = append(out, adminRequestRow{TgID: id, Username: r.username, FirstName: r.firstName, Login: r.login, At: r.at})
	}
	return out
}

type adminRequestRow struct {
	TgID      int64
	Username  string
	FirstName string
	Login     string
	At        time.Time
}

func (b *AdminBot) cmdUnlink(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin) {
	if err := b.dropAdminChat(ctx, chat); err != nil {
		return
	}
	b.safeAudit(ctx, admin.ID, "adminbot.unlink", "admin", admin.Username, nil)
	b.say(out, chat, w.unlinked, nil)
}

// --- callbacks ---

func (b *AdminBot) answer(c *Client, qid, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = c.Answer(ctx, qid, text)
}

func (b *AdminBot) onAdminPress(ctx context.Context, c *Client, out *Outbox, q *CallbackQuery) {
	chat := q.Message.Chat.ID
	if b.flooding(chat) {
		return
	}
	w := b.langWords(ctx)
	parts := strings.Split(q.Data, ":")
	if len(parts) < 2 || parts[0] != "ab" {
		return
	}
	// Approve/refuse taps come from the owner; everything else needs a linked admin.
	switch parts[1] {
	case "approve", "refuse":
		b.onApproveRefuse(ctx, c, out, w, q, parts)
		return
	}
	admin, err := b.identity(ctx, chat)
	if err != nil {
		b.answer(c, q.ID, w.needLink)
		return
	}
	switch parts[1] {
	case "user":
		if len(parts) != 3 {
			return
		}
		uid, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return
		}
		u, err := b.d.Users.Get(ctx, uid)
		if err != nil {
			b.answer(c, q.ID, w.userNotFound)
			return
		}
		b.showUser(ctx, c, out, w, chat, admin, u, q)
	case "m":
		if len(parts) != 3 {
			return
		}
		b.menuTap(ctx, c, out, w, q, chat, admin, parts[2])
	case "page":
		if len(parts) != 3 {
			return
		}
		b.pageTap(ctx, c, out, w, q, chat, admin, parts[2])
	case "cf", "do":
		if len(parts) < 4 {
			return
		}
		arg := strings.Join(parts[3:], ":")
		if parts[1] == "cf" {
			b.askConfirm(ctx, c, out, w, q, admin, parts[2], arg)
		} else {
			b.doAction(ctx, c, out, w, q, chat, admin, parts[2], arg)
		}
	case "unb":
		if len(parts) != 3 {
			return
		}
		b.pickUnbind(ctx, c, out, w, q, chat, admin, parts[2])
	case "bc":
		if len(parts) != 3 {
			return
		}
		b.doBroadcast(ctx, c, out, w, q, chat, admin, parts[2])
	case "ul":
		if len(parts) != 4 {
			return
		}
		page, _ := strconv.Atoi(parts[3])
		b.cmdUsers(ctx, out, w, chat, admin, parts[2], page)
	case "uf":
		if len(parts) != 3 {
			return
		}
		b.cmdUsers(ctx, out, w, chat, admin, parts[2], 0)
	case "ux":
		if len(parts) != 4 {
			return
		}
		b.userSection(ctx, c, out, w, q, chat, admin, parts[2], parts[3])
	case "xe":
		if len(parts) != 3 {
			return
		}
		b.extendView(ctx, out, w, chat, admin, parts[2])
	case "x":
		if len(parts) != 4 {
			return
		}
		days, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return
		}
		uid, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return
		}
		b.doExtendDays(ctx, out, w, chat, admin, uid, days)
	case "xm":
		if len(parts) != 4 {
			return
		}
		m, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return
		}
		uid, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return
		}
		b.doExtendMonths(ctx, out, w, chat, admin, uid, m)
	case "xp":
		if len(parts) != 4 {
			return
		}
		uid, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return
		}
		tid, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return
		}
		u, err := b.d.Users.Get(ctx, uid)
		if err != nil {
			b.answer(c, q.ID, w.userNotFound)
			return
		}
		_ = u
		b.askConfirm(ctx, c, out, w, q, admin, "tariff", strconv.FormatInt(uid, 10)+":"+strconv.FormatInt(tid, 10))
	case "xl":
		if len(parts) != 4 {
			return
		}
		uid, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return
		}
		b.doLimit(ctx, out, w, chat, admin, uid, parts[3])
	case "xi":
		if len(parts) != 4 {
			return
		}
		uid, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return
		}
		inbid, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return
		}
		b.doToggleInbound(ctx, out, w, chat, admin, uid, inbid)
	case "xn":
		if len(parts) != 3 {
			return
		}
		tid, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return
		}
		b.createFromStash(ctx, out, w, chat, admin, tid)
	case "xw":
		if len(parts) != 3 {
			return
		}
		b.revealLinkTap(ctx, c, out, w, q, chat, admin, parts[2])
	case "xk":
		if len(parts) != 3 {
			return
		}
		b.revealKeysTap(ctx, c, out, w, q, chat, admin, parts[2])
	case "xv":
		if len(parts) != 3 {
			return
		}
		b.avatarTap(ctx, c, out, w, q, chat, admin, parts[2])
	case "xnote":
		if len(parts) != 3 {
			return
		}
		b.askFreeText(ctx, c, out, w, q, chat, admin, "note:"+parts[2], w.noteAsk)
	case "xcontact":
		if len(parts) != 3 {
			return
		}
		b.askFreeText(ctx, c, out, w, q, chat, admin, "contact:"+parts[2], w.contactAsk)
	case "xdate":
		if len(parts) != 3 {
			return
		}
		b.askFreeText(ctx, c, out, w, q, chat, admin, "expdate:"+parts[2], w.dateAsk)
	case "xbill":
		if len(parts) != 3 {
			return
		}
		b.askFreeText(ctx, c, out, w, q, chat, admin, "billday:"+parts[2], w.billAsk)
	default:
		b.answer(c, q.ID, w.unknown)
	}
}

func (b *AdminBot) tapperOwner(ctx context.Context, chat int64) (db.Admin, bool) {
	admin, err := b.identity(ctx, chat)
	if err != nil || admin.IsOwner == 0 {
		return db.Admin{}, false
	}
	return admin, true
}

func (b *AdminBot) onApproveRefuse(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, parts []string) {
	chat := q.Message.Chat.ID
	owner, ok := b.tapperOwner(ctx, chat)
	if !ok {
		b.answer(c, q.ID, w.needOwner)
		return
	}
	if len(parts) != 3 {
		return
	}
	tg, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return
	}
	b.mu.Lock()
	_, found := b.requests[tg]
	b.mu.Unlock()
	if !found {
		_ = c.Edit(ctx, chat, q.Message.MessageID, w.requestGone, nil)
		b.answer(c, q.ID, "")
		return
	}
	if parts[1] == "refuse" {
		existed := b.RefuseRequest(tg)
		_ = c.Edit(ctx, chat, q.Message.MessageID, w.refused, nil)
		b.answer(c, q.ID, "")
		if existed {
			b.say(out, tg, w.requestRefused, nil)
		}
		return
	}
	target, err := b.ApproveRequest(ctx, tg)
	if err != nil {
		_ = c.Edit(ctx, chat, q.Message.MessageID, w.requestGone, nil)
		b.answer(c, q.ID, "")
		return
	}
	b.safeAudit(ctx, owner.ID, "adminbot.approve", "admin", target.Username, map[string]any{"tg_id": tg})
	_ = c.Edit(ctx, chat, q.Message.MessageID, w.approved, nil)
	b.answer(c, q.ID, "")
	b.say(out, tg, w.requestApproved, nil)
}

func (b *AdminBot) safeAudit(ctx context.Context, adminID int64, action, targetType, targetID string, details map[string]any) {
	if err := audit.Write(ctx, b.d.Store.Q, b.d.Now(), audit.Entry{AdminID: adminID, Action: action, TargetType: targetType, TargetID: targetID, Details: details}); err != nil {
		b.d.Log.Warn("adminbot: audit write failed", "action", action, "err", err)
	}
}

// check enforces one command's matrix entry.
func (b *AdminBot) check(out *Outbox, w adminWords, chat int64, admin db.Admin, tag string, write bool) bool {
	if err := adminCan(admin, tag, write); err != nil {
		b.deny(out, chat, w, err)
		return false
	}
	return true
}

func fmtDate(unix int64) string {
	if unix <= 0 {
		return "—"
	}
	return time.Unix(unix, 0).UTC().Format("02.01.2006")
}

func userStateText(u db.User) string {
	if u.Status == "disabled" {
		return "⛔"
	}
	return "✅"
}

// onlineOf says whether any of the user's slots is live now.
func (b *AdminBot) onlineOf(ctx context.Context, userID int64) bool {
	if b.d.Online == nil {
		return false
	}
	slots, err := b.d.Store.Q.ListUserSlots(ctx, userID)
	if err != nil {
		return false
	}
	live := b.d.Online()
	for _, s := range slots {
		if _, ok := live[s]; ok {
			return true
		}
	}
	return false
}

func (b *AdminBot) cmdStats(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin) {
	if !b.check(out, w, chat, admin, "stats", false) {
		return
	}
	users, err := b.d.Store.Q.ListUsers(ctx)
	if err != nil {
		return
	}
	online := 0
	if b.d.Online != nil {
		rows, err := b.d.Store.Q.ListSlotUsers(ctx)
		if err != nil {
			return
		}
		byUser := map[int64][]string{}
		for _, r := range rows {
			byUser[r.UserID] = append(byUser[r.UserID], r.SlotName)
		}
		live := b.d.Online()
		for _, names := range byUser {
			for _, n := range names {
				if _, ok := live[n]; ok {
					online++
					break
				}
			}
		}
	}
	nodes := 0
	if b.d.Store != nil {
		if list, err := b.d.Store.Q.ListNodes(ctx); err == nil {
			nodes = len(list)
		}
	}
	b.say(out, chat, render(w.stats, map[string]string{
		"users":  strconv.Itoa(len(users)),
		"online": strconv.Itoa(online),
		"nodes":  strconv.Itoa(nodes),
	}), nil)
}

func (b *AdminBot) cmdUser(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, q string) {
	if !b.check(out, w, chat, admin, "users", false) {
		return
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		b.say(out, chat, w.userNotFound, nil)
		return
	}
	users, err := b.d.Store.Q.ListUsers(ctx)
	if err != nil {
		return
	}
	var found []db.User
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.Name), q) || strings.Contains(strings.ToLower(u.Contact), q) || strconv.FormatInt(u.ID, 10) == q {
			found = append(found, u)
			if len(found) > 5 {
				break
			}
		}
	}
	if len(found) == 0 {
		b.say(out, chat, w.userNotFound, nil)
		return
	}
	if len(found) == 1 {
		u, err := b.d.Users.Get(ctx, found[0].ID)
		if err != nil {
			return
		}
		b.showUser(ctx, nil, out, w, chat, admin, u, nil)
		return
	}
	kb := &Keyboard{}
	text := "🔎"
	for i, u := range found {
		if i >= 5 {
			text += "\n" + render(w.userMany, map[string]string{"n": strconv.Itoa(len(found) - 5)})
			break
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{Text: "#" + strconv.FormatInt(u.ID, 10) + " " + u.Name, CallbackData: "ab:user:" + strconv.FormatInt(u.ID, 10)}})
	}
	b.say(out, chat, text, kb)
}

func opVerb(w adminWords, op string) string {
	switch op {
	case "disable":
		return w.disableVerb
	case "enable":
		return w.enableVerb
	case "extend":
		return w.extendVerb
	case "reset":
		return w.resetVerb
	case "unbind":
		return w.unbindVerb
	case "delete":
		return w.deleteVerb
	case "reissue":
		return w.reissueVerb
	case "tariff":
		return w.tariffVerb
	case "tgunlink":
		return w.tgunlinkVerb
	case "forever":
		return w.foreverVerb
	}
	return op
}

func (b *AdminBot) askConfirm(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, admin db.Admin, op, arg string) {
	uid, err := strconv.ParseInt(strings.Split(arg, ":")[0], 10, 64)
	if err != nil {
		return
	}
	u, err := b.d.Users.Get(ctx, uid)
	if err != nil {
		b.answer(c, q.ID, w.userNotFound)
		return
	}
	_ = admin
	kb := &Keyboard{InlineKeyboard: [][]Button{
		{{Text: "✅", CallbackData: "ab:do:" + op + ":" + arg},
			{Text: "🚫", CallbackData: "ab:user:" + strconv.FormatInt(u.ID, 10)}},
	}}
	out.Reply(q.Message.Chat.ID, "ab:cf:"+op+":"+arg, 1, func(ctx context.Context, c *Client) error {
		_, err := c.Send(ctx, q.Message.Chat.ID, render(w.confirmAsk, map[string]string{"what": opVerb(w, op), "name": u.Name}), kb, false)
		return err
	})
}

func (b *AdminBot) doAction(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, chat int64, admin db.Admin, op, arg string) {
	if !b.check(out, w, chat, admin, "users", true) {
		b.answer(c, q.ID, w.noAccess)
		return
	}
	uid, err := strconv.ParseInt(strings.Split(arg, ":")[0], 10, 64)
	if err != nil {
		return
	}
	var done, auditAction string
	switch op {
	case "enable", "disable":
		off := op == "disable"
		if _, err := b.d.Users.Update(ctx, uid, domain.Patch{Disabled: &off}); err != nil {
			return
		}
		done, auditAction = w.actEnabled, "adminbot.user_enable"
		if off {
			done, auditAction = w.actDisabled, "adminbot.user_disable"
		}
	case "extend":
		if _, err := b.d.Users.Extend(ctx, uid, 30); err != nil {
			return
		}
		done, auditAction = w.actExtended, "adminbot.user_extend"
	case "reset":
		if _, err := b.d.Users.ResetTraffic(ctx, uid); err != nil {
			return
		}
		done, auditAction = w.actReset, "adminbot.user_reset"
	case "unbind":
		parts := strings.Split(arg, ":")
		if len(parts) != 2 {
			return
		}
		dev, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return
		}
		if err := b.d.Devices.Unbind(ctx, uid, dev, false); err != nil {
			return
		}
		done, auditAction = w.actUnbound, "adminbot.user_unbind"
	case "delete":
		old := b.avatarNameOf(ctx, uid)
		if err := b.d.Users.Delete(ctx, uid); err != nil {
			return
		}
		b.dropAvatarFile(old)
		done, auditAction = w.applied, "adminbot.user_delete"
	case "reissue":
		if _, err := b.d.Users.Reissue(ctx, uid); err != nil {
			return
		}
		done, auditAction = w.applied, "adminbot.user_reissue"
	case "tariff":
		parts := strings.Split(arg, ":")
		if len(parts) != 2 {
			return
		}
		tid, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return
		}
		if _, err := b.d.Users.Update(ctx, uid, domain.Patch{TariffID: &tid}); err != nil {
			return
		}
		done, auditAction = w.applied, "adminbot.user_tariff"
	case "tgunlink":
		if err := b.d.Store.Q.UnlinkTg(ctx, uid); err != nil {
			return
		}
		done, auditAction = w.applied, "adminbot.user_tgunlink"
	case "forever":
		if _, err := b.d.Users.Update(ctx, uid, domain.Patch{ClearExpiry: true}); err != nil {
			return
		}
		done, auditAction = w.applied, "adminbot.user_forever"
	default:
		return
	}
	b.safeAudit(ctx, admin.ID, auditAction, "user", strconv.FormatInt(uid, 10), nil)
	b.say(out, chat, done, nil)
}

func boundLabel(hwid, os, osv, model, app string, id int64) string {
	name := model
	if name == "" {
		name = strings.TrimSpace(os + " " + osv)
	}
	if name == "" {
		name = app
	}
	if name == "" {
		name = "#" + strconv.FormatInt(id, 10)
	}
	if hwid == "" {
		name += " (shared)"
	}
	return name
}

func (b *AdminBot) pickUnbind(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, chat int64, admin db.Admin, uidStr string) {
	if !b.check(out, w, chat, admin, "users", true) {
		b.answer(c, q.ID, w.noAccess)
		return
	}
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		return
	}
	devs, err := b.d.Store.Q.ListBoundDevices(ctx, uid)
	if err != nil || len(devs) == 0 {
		b.say(out, chat, w.noBound, nil)
		return
	}
	kb := &Keyboard{}
	for _, d := range devs {
		kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{
			Text:         boundLabel(d.Hwid, d.Os, d.OsVersion, d.Model, d.App, d.ID),
			CallbackData: "ab:cf:unbind:" + strconv.FormatInt(uid, 10) + ":" + strconv.FormatInt(d.ID, 10),
		}})
	}
	b.say(out, chat, w.unbindPick, kb)
}

func (b *AdminBot) cmdNodes(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin) {
	if !b.check(out, w, chat, admin, "node", false) {
		return
	}
	nodes, err := b.d.Store.Q.ListNodes(ctx)
	if err != nil || len(nodes) == 0 {
		b.say(out, chat, w.noNodes, nil)
		return
	}
	var lines []string
	lines = append(lines, "<b>"+esc(w.nodesTitle)+"</b>")
	for _, n := range nodes {
		mark := "🔴"
		extra := ""
		if n.Enabled == 0 {
			extra = " (off)"
		} else if b.d.Health != nil {
			if ok, msg := b.d.Health(n.ID); ok {
				mark = "🟢"
			} else if msg != "" {
				extra = ": " + esc(msg)
			}
		} else {
			mark = "⚪"
		}
		lines = append(lines, mark+" "+esc(n.Name)+extra)
	}
	b.say(out, chat, strings.Join(lines, "\n"), nil)
}

func (b *AdminBot) cmdBroadcast(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, text string) {
	if admin.IsOwner == 0 {
		b.deny(out, chat, w, errAdminNoGrant)
		return
	}
	if text == "" {
		b.awaitAsk(chat, "broadcast")
		b.say(out, chat, w.askBcast, nil)
		return
	}
	targets, err := b.adminTargets(ctx)
	if err != nil {
		return
	}
	b.mu.Lock()
	b.pending[chat] = text
	b.mu.Unlock()
	kb := &Keyboard{InlineKeyboard: [][]Button{
		{{Text: "✅", CallbackData: "ab:bc:go"},
			{Text: "🚫", CallbackData: "ab:bc:no"}},
	}}
	if len([]rune(text)) > 500 {
		text = string([]rune(text)[:500]) + "…"
	}
	b.say(out, chat, render(w.broadcastAsk, map[string]string{"n": strconv.Itoa(len(targets)), "text": text}), kb)
}

func (b *AdminBot) doBroadcast(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, chat int64, admin db.Admin, what string) {
	if admin.IsOwner == 0 {
		b.answer(c, q.ID, w.noAccess)
		return
	}
	b.mu.Lock()
	text, ok := b.pending[chat]
	if ok {
		delete(b.pending, chat)
	}
	b.mu.Unlock()
	if what != "go" || !ok {
		b.say(out, chat, w.cancelled, nil)
		return
	}
	n, err := b.Broadcast(ctx, text)
	if err != nil {
		return
	}
	b.safeAudit(ctx, admin.ID, "adminbot.broadcast", "", "", map[string]any{"queued": n})
	b.say(out, chat, render(w.broadcastSent, map[string]string{"n": strconv.Itoa(n)}), nil)
}

// menuTap runs a menu button. Grants are checked again: the menu filters, the
// tap enforces.
func (b *AdminBot) menuTap(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, chat int64, admin db.Admin, action string) {
	b.answer(c, q.ID, "")
	switch action {
	case AdminActStats:
		b.cmdStats(ctx, out, w, chat, admin)
	case AdminActFind:
		if !b.check(out, w, chat, admin, "users", false) {
			return
		}
		b.awaitAsk(chat, "find")
		b.say(out, chat, w.askQuery, nil)
	case AdminActNodes:
		b.cmdNodes(ctx, out, w, chat, admin)
	case AdminActBcast:
		if admin.IsOwner == 0 {
			b.say(out, chat, w.noAccess, nil)
			return
		}
		b.awaitAsk(chat, "broadcast")
		b.say(out, chat, w.askBcast, nil)
	case AdminActRequests:
		b.listRequests(ctx, out, w, chat, admin)
	case AdminActUnlink:
		b.cmdUnlink(ctx, out, w, chat, admin)
	case AdminActHelp:
		b.say(out, chat, menuHelpText(admin, b.AdminMenuConfig(ctx), w), nil)
	default:
		b.say(out, chat, w.unknown, nil)
	}
}

// pageTap sends the owner's page text.
func (b *AdminBot) pageTap(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, chat int64, admin db.Admin, id string) {
	b.answer(c, q.ID, "")
	cfg := b.AdminMenuConfig(ctx)
	for _, btn := range cfg.Buttons {
		if btn.ID == id && btn.On && btn.Action == "page" {
			b.say(out, chat, btn.Text, nil)
			return
		}
	}
	b.say(out, chat, w.unknown, nil)
}

// listRequests shows pending link requests with approve buttons (owner only).
func (b *AdminBot) listRequests(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin) {
	if admin.IsOwner == 0 {
		b.say(out, chat, w.noAccess, nil)
		return
	}
	reqs := b.LinkRequests()
	if len(reqs) == 0 {
		b.say(out, chat, w.noRequests, nil)
		return
	}
	for _, r := range reqs {
		who := "@" + r.Username
		if r.Username == "" {
			who = r.FirstName
		}
		kb := &Keyboard{InlineKeyboard: [][]Button{
			{{Text: "✅", CallbackData: "ab:approve:" + strconv.FormatInt(r.TgID, 10)},
				{Text: "🚫", CallbackData: "ab:refuse:" + strconv.FormatInt(r.TgID, 10)}},
		}}
		b.say(out, chat, w.approveAsk+" <b>"+esc(who)+"</b> → <b>"+esc(r.Login)+"</b>", kb)
	}
}
