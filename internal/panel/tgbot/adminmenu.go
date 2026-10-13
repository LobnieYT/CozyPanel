package tgbot

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store/db"
)

// The admin menu: what the owner sets up in the panel, mirroring the
// subscriber bot's menu and texts. Stored as one JSON setting.

// KeyAdminMenu is the admin menu and texts.
const KeyAdminMenu = "tg_admin_config"

// AdminMenuButton actions. Built-ins run bot flows; "url" opens a link, "page"
// sends the owner's text.
const (
	AdminActStats    = "stats"
	AdminActFind     = "find"
	AdminActNodes    = "nodes"
	AdminActBcast    = "broadcast"
	AdminActRequests = "requests"
	AdminActUnlink   = "unlink"
	AdminActHelp     = "help"
)

var adminBuiltins = []string{AdminActStats, AdminActFind, AdminActNodes, AdminActBcast, AdminActRequests, AdminActUnlink, AdminActHelp}

type AdminMenuButton struct {
	ID     string `json:"id" doc:"Постоянный id кнопки"`
	Action string `json:"action" enum:"stats,find,nodes,broadcast,requests,unlink,help,url,page"`
	Label  string `json:"label"`
	On     bool   `json:"on"`
	Row    bool   `json:"row" doc:"В одном ряду с предыдущей"`
	URL    string `json:"url,omitempty" doc:"Для action=url: https:// или tg://"`
	Text   string `json:"text,omitempty" doc:"Для action=page: текст страницы"`
}

// AdminMenuTexts the owner writes. An empty text takes the built-in one.
type AdminMenuTexts struct {
	Menu string `json:"menu" doc:"Шапка меню"`
	Help string `json:"help" doc:"Текст /help"`
}

type AdminMenu struct {
	Lang    string            `json:"lang" enum:"ru,en" doc:"Язык встроенных надписей бота"`
	Buttons []AdminMenuButton `json:"buttons" doc:"Кнопки меню по порядку"`
	Texts   AdminMenuTexts    `json:"texts"`
}

// DefaultAdminMenu is the menu of a fresh bot in lang, "en" or else Russian.
func DefaultAdminMenu(lang string) AdminMenu {
	if lang != "en" {
		lang = "ru"
	}
	l := func(ru, en string) string {
		if lang == "en" {
			return en
		}
		return ru
	}
	return AdminMenu{
		Lang: lang,
		Buttons: []AdminMenuButton{
			{ID: AdminActStats, Action: AdminActStats, Label: l("📊 Статистика", "📊 Stats"), On: true},
			{ID: AdminActFind, Action: AdminActFind, Label: l("👥 Найти юзера", "👥 Find user"), On: true, Row: true},
			{ID: AdminActNodes, Action: AdminActNodes, Label: l("🖥 Ноды", "🖥 Nodes"), On: true},
			{ID: AdminActBcast, Action: AdminActBcast, Label: l("📣 Рассылка", "📣 Broadcast"), On: true, Row: true},
			{ID: AdminActRequests, Action: AdminActRequests, Label: l("📝 Заявки", "📝 Requests"), On: true},
			{ID: AdminActUnlink, Action: AdminActUnlink, Label: l("🔓 Отвязать чат", "🔓 Unlink chat"), On: true, Row: true},
			{ID: AdminActHelp, Action: AdminActHelp, Label: l("❓ Помощь", "❓ Help"), On: true},
		},
	}
}

// Validate checks a menu from the admin panel and fills in defaults. Custom
// texts must not carry raw angle brackets: every bot message goes out as
// Telegram HTML, and a literal <placeholder> kills the send for every chat.
func (m *AdminMenu) Validate() error {
	if m.Lang != "en" {
		m.Lang = "ru"
	}
	if len(m.Buttons) > maxButtons {
		return ErrButtons
	}
	seen := map[string]bool{}
	for i := range m.Buttons {
		b := &m.Buttons[i]
		b.Label = strings.TrimSpace(b.Label)
		if b.Label == "" || utf8.RuneCountInString(b.Label) > maxLabel {
			return ErrButtonLabel
		}
		switch b.Action {
		case "url":
			if !safeURL(b.URL) {
				return ErrButtonURL
			}
		case "page":
			if strings.TrimSpace(b.Text) == "" || utf8.RuneCountInString(b.Text) > maxText {
				return ErrButtonText
			}
		default:
			if !contains(adminBuiltins, b.Action) || seen[b.Action] {
				return ErrButtons
			}
			seen[b.Action] = true
			b.ID = b.Action
		}
		if b.Action == "url" || b.Action == "page" {
			if b.ID == "" || contains(adminBuiltins, b.ID) || len(b.ID) > 16 || strings.ContainsAny(b.ID, ": ") || seen["id:"+b.ID] {
				b.ID = "c" + itoa(i+1)
			}
			seen["id:"+b.ID] = true
			if b.Action == "url" {
				b.Text = ""
			} else {
				b.URL = ""
			}
		}
	}
	for _, t := range []string{m.Texts.Menu, m.Texts.Help} {
		if utf8.RuneCountInString(t) > maxText {
			return ErrText
		}
		if strings.ContainsAny(t, "<>") {
			return ErrText
		}
	}
	return nil
}

// AdminMenuConfig is the effective menu: saved over the default.
func (b *AdminBot) AdminMenuConfig(ctx context.Context) AdminMenu {
	lang, _ := b.d.Settings.Lang(ctx)
	def := DefaultAdminMenu(lang)
	cfg, ok, err := settings.GetOver(ctx, b.d.Settings, KeyAdminMenu, def)
	if err != nil || !ok {
		return def
	}
	if err := cfg.Validate(); err != nil {
		return def
	}
	return cfg
}

// menuAllowed says whether the admin may tap the action: the matrix, read as
// strictly as the commands. url/page carry the owner's own content.
func menuAllowed(admin db.Admin, action string) bool {
	switch action {
	case AdminActStats:
		return adminCan(admin, "stats", false) == nil
	case AdminActFind:
		return adminCan(admin, "users", false) == nil
	case AdminActNodes:
		return adminCan(admin, "node", false) == nil
	case AdminActBcast, AdminActRequests:
		return admin.IsOwner != 0
	case AdminActUnlink, AdminActHelp, "url", "page":
		return true
	}
	return false
}

// menuKeyboard lays the configured buttons out, skipping what is off or
// forbidden. Row continues the previous row, like the subscriber menu.
func menuKeyboard(admin db.Admin, cfg AdminMenu) *Keyboard {
	kb := &Keyboard{}
	var row []Button
	flush := func() {
		if len(row) > 0 {
			kb.InlineKeyboard = append(kb.InlineKeyboard, row)
			row = nil
		}
	}
	for _, b := range cfg.Buttons {
		if !b.On || !menuAllowed(admin, b.Action) {
			continue
		}
		var btn Button
		switch b.Action {
		case "url":
			btn = Button{Text: b.Label, URL: b.URL}
		case "page":
			btn = Button{Text: b.Label, CallbackData: "ab:page:" + b.ID}
		default:
			btn = Button{Text: b.Label, CallbackData: "ab:m:" + b.Action}
		}
		if !b.Row || len(row) == 0 {
			flush()
		}
		row = append(row, btn)
	}
	flush()
	if len(kb.InlineKeyboard) == 0 {
		return nil
	}
	return kb
}

// menuHelpText is the help body: the owner's override or the built-in one
// (plus the owner suffix for the owner).
func menuHelpText(admin db.Admin, cfg AdminMenu, w adminWords) string {
	if strings.TrimSpace(cfg.Texts.Help) != "" {
		if admin.IsOwner != 0 {
			return cfg.Texts.Help + w.helpOwner
		}
		return cfg.Texts.Help
	}
	if admin.IsOwner != 0 {
		return w.help + w.helpOwner
	}
	return w.help
}

// sendMenu shows the menu: header plus the configured buttons.
func (b *AdminBot) sendMenu(ctx context.Context, out *Outbox, chat int64, admin db.Admin) {
	w := b.langWords(ctx)
	cfg := b.AdminMenuConfig(ctx)
	head := cfg.Texts.Menu
	if strings.TrimSpace(head) == "" {
		head = w.menuHead
	}
	b.say(out, chat, head, menuKeyboard(admin, cfg))
}

// awaitAsk remembers what the chat's next free text answers.
func (b *AdminBot) awaitAsk(chat int64, what string) {
	b.mu.Lock()
	b.requestsPruneLocked(b.d.Now())
	b.awaiting[chat] = awaitKind{what: what, at: b.d.Now()}
	b.mu.Unlock()
}

// takeAwait takes a fresh answer, "" when none waits (or it expired, or the
// chat taps a command instead, which clears it).
func (b *AdminBot) takeAwait(chat int64, isCommand bool) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	a, ok := b.awaiting[chat]
	delete(b.awaiting, chat)
	if !ok || isCommand || b.d.Now().Sub(a.at) > awaitTTL {
		return ""
	}
	return a.what
}

func (b *AdminBot) requestsPruneLocked(now time.Time) {
	for id, r := range b.requests {
		if now.Sub(r.at) > requestTTL {
			delete(b.requests, id)
		}
	}
	for id, a := range b.awaiting {
		if now.Sub(a.at) > awaitTTL {
			delete(b.awaiting, id)
		}
	}
}
