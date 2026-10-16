package tgbot

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cozy/internal/panel/domain"
	"cozy/internal/panel/store/db"
	"cozy/internal/panel/subs"
)

// The Users section in the bot: list with state filters, full-text search,
// rich cards and every non-bulk mutation of the panel. Reads need users/r,
// writes users/w; destructive ones ask twice, like the panel's Confirms.

const userPageSize = 8

var userFilters = []string{"all", domain.StateActive, domain.StateExpiring, domain.StateLimited, domain.StateExpired, domain.StateDisabled}

// userStates loads every user with its panel state for list counts.
func (b *AdminBot) userStates(ctx context.Context) ([]db.User, map[int64]string, error) {
	users, err := b.d.Store.Q.ListUsers(ctx)
	if err != nil {
		return nil, nil, err
	}
	grants, err := domain.LoadGrantsLeft(ctx, b.d.Store.Q, b.d.Now())
	if err != nil {
		return nil, nil, err
	}
	now := b.d.Now()
	states := make(map[int64]string, len(users))
	for _, u := range users {
		states[u.ID] = domain.State(u, grants.Main(u.ID), now)
	}
	return users, states, nil
}

func stateMark(state string) string {
	switch state {
	case domain.StateActive:
		return "✅"
	case domain.StateExpiring:
		return "⏳"
	case domain.StateLimited:
		return "🧱"
	case domain.StateExpired:
		return "⌛"
	case domain.StateDisabled:
		return "⛔"
	}
	return "❓"
}

func filterLabel(w adminWords, f string) string {
	switch f {
	case "all":
		return w.fAll
	case domain.StateActive:
		return w.fActive
	case domain.StateExpiring:
		return w.fExpiring
	case domain.StateLimited:
		return w.fLimited
	case domain.StateExpired:
		return w.fExpired
	case domain.StateDisabled:
		return w.fDisabled
	}
	return f
}

// cmdUsers lists users with a state filter and a page.
func (b *AdminBot) cmdUsers(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, filter string, page int) {
	if !b.check(out, w, chat, admin, "users", false) {
		return
	}
	users, states, err := b.userStates(ctx)
	if err != nil {
		return
	}
	counts := map[string]int{}
	for _, u := range users {
		counts[states[u.ID]]++
	}
	var shown []db.User
	for _, u := range users {
		if filter == "all" || states[u.ID] == filter {
			shown = append(shown, u)
		}
	}
	pages := (len(shown) + userPageSize - 1) / userPageSize
	if pages < 1 {
		pages = 1
	}
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	var lines []string
	lines = append(lines, "<b>"+esc(w.usersTitle)+"</b> "+filterLabel(w, filter)+
		" ("+strconv.Itoa(len(shown))+"/"+strconv.Itoa(len(users))+")")
	end := min(page*userPageSize+userPageSize, len(shown))
	kb := &Keyboard{}
	for _, u := range shown[page*userPageSize : end] {
		kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{
			Text:         stateMark(states[u.ID]) + " " + u.Name,
			CallbackData: "ab:user:" + strconv.FormatInt(u.ID, 10),
		}})
	}
	var frow []Button
	for _, f := range userFilters {
		label := filterLabel(w, f)
		if f == filter {
			label = "• " + label + " (" + strconv.Itoa(counts[f]) + ")"
		}
		frow = append(frow, Button{Text: label, CallbackData: "ab:ul:" + f + ":0"})
		if len(frow) == 3 {
			kb.InlineKeyboard = append(kb.InlineKeyboard, frow)
			frow = nil
		}
	}
	if len(frow) > 0 {
		kb.InlineKeyboard = append(kb.InlineKeyboard, frow)
	}
	var prow []Button
	if page > 0 {
		prow = append(prow, Button{Text: "◀", CallbackData: "ab:ul:" + filter + ":" + strconv.Itoa(page-1)})
	}
	if page < pages-1 {
		prow = append(prow, Button{Text: "▶", CallbackData: "ab:ul:" + filter + ":" + strconv.Itoa(page+1)})
	}
	if len(prow) > 0 {
		kb.InlineKeyboard = append(kb.InlineKeyboard, prow)
	}
	b.say(out, chat, strings.Join(lines, "\n"), kb)
}

// maskIP hides the second half of an address, like the panel.
func maskIP(ip string) string {
	if strings.Contains(ip, ":") {
		parts := strings.Split(ip, ":")
		if len(parts) > 3 {
			return strings.Join(parts[:3], ":") + ":…"
		}
		return ip
	}
	p := strings.Split(ip, ".")
	if len(p) == 4 {
		return p[0] + "." + p[1] + ".•••.•••"
	}
	return ip
}

// trafficText summarizes today, the total and the last 7 days.
func (b *AdminBot) trafficText(ctx context.Context, w adminWords, uid int64) string {
	now := b.d.Now()
	day := now.Unix() / 86400
	rows, err := b.d.Store.Q.UserTrafficDaily(ctx, db.UserTrafficDailyParams{UserID: uid, Day: day - 6})
	if err != nil {
		return ""
	}
	byDay := map[int64]db.UserTrafficDailyRow{}
	var totUp, totDown int64
	for _, r := range rows {
		byDay[r.Day] = r
		totUp += r.Up
		totDown += r.Down
	}
	var lines []string
	if r, ok := byDay[day]; ok {
		lines = append(lines, render(w.trToday, map[string]string{"up": fmtBytes(r.Up), "down": fmtBytes(r.Down)}))
	}
	var bars []string
	for i := int64(6); i >= 0; i-- {
		r := byDay[day-i]
		bars = append(bars, spark(r.Up+r.Down))
	}
	lines = append(lines, render(w.trWeek, map[string]string{"up": fmtBytes(totUp), "down": fmtBytes(totDown), "bars": strings.Join(bars, "")}))
	return strings.Join(lines, "\n")
}

func spark(n int64) string {
	switch {
	case n <= 0:
		return "▁"
	case n < 100<<20:
		return "▂"
	case n < 1024<<20:
		return "▃"
	case n < 10<<30:
		return "▅"
	default:
		return "█"
	}
}

// showUser renders the rich card: state, traffic, term, online, devices,
// tariff, note snippet and the action rows.
func (b *AdminBot) showUser(ctx context.Context, c *Client, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User, q *CallbackQuery) {
	if !b.check(out, w, chat, admin, "users", false) {
		if q != nil && c != nil {
			b.answer(c, q.ID, w.noAccess)
		}
		return
	}
	fresh, err := b.d.Users.Get(ctx, u.ID)
	if err == nil {
		u = fresh
	}
	online := ""
	if b.onlineOf(ctx, u.ID) {
		online = "\n🟢 online"
	}
	limit := "∞"
	if u.TrafficLimit.Valid {
		limit = fmtBytes(u.TrafficLimit.Int64)
	}
	tariff := "—"
	if u.TariffID.Valid {
		if list, err := b.d.Store.Q.ListTariffs(ctx); err == nil {
			for _, t := range list {
				if t.ID == u.TariffID.Int64 {
					tariff = t.Name
				}
			}
		}
	}
	boundN, addrsN := 0, 0
	if devs, err := b.d.Store.Q.ListBoundDevices(ctx, u.ID); err == nil {
		boundN = len(devs)
	}
	if addrs, err := b.d.Store.Q.ListUserDevices(ctx, u.ID); err == nil {
		addrsN = len(addrs)
	}
	note := ""
	if strings.TrimSpace(u.Note) != "" {
		note = "\n📝 " + truncateRunes(u.Note, 120)
	}
	resets := ""
	if at, ok := domain.NextReset(u, b.d.Now()); ok {
		resets = " (сброс " + at.UTC().Format("02.01") + ")"
	}
	uid := strconv.FormatInt(u.ID, 10)
	text := render(w.userTitle, map[string]string{"id": uid, "name": u.Name}) + "\n" +
		userStateText(u) + " " + esc(u.Contact) + note +
		"\n📊 " + fmtBytes(u.UsedUp+u.UsedDown) + " / " + limit +
		"\n📅 " + fmtDate(u.ExpiresAt.Int64) + resets + online +
		"\n📦 " + esc(tariff) + " · 📱" + strconv.Itoa(boundN) + " · 🌐" + strconv.Itoa(addrsN)
	toggle, toggleOp := w.disableVerb, "disable"
	if u.Status == "disabled" {
		toggle, toggleOp = w.enableVerb, "enable"
	}
	kb := &Keyboard{InlineKeyboard: [][]Button{
		{{Text: toggle, CallbackData: "ab:cf:" + toggleOp + ":" + uid},
			{Text: w.extendVerb, CallbackData: "ab:xe:" + uid}},
		{{Text: w.resetVerb, CallbackData: "ab:cf:reset:" + uid},
			{Text: "📊", CallbackData: "ab:ux:t:" + uid},
			{Text: "📱", CallbackData: "ab:ux:d:" + uid}},
		{{Text: w.moreActions, CallbackData: "ab:ux:m:" + uid}},
	}}
	if q == nil {
		b.say(out, chat, text, kb)
		return
	}
	out.Reply(chat, "ab:user:"+uid, 1, func(ctx context.Context, c *Client) error {
		_, err := c.Send(ctx, chat, text, kb, false)
		return err
	})
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// moreView is the second row of actions: everything the card did not fit.
func (b *AdminBot) moreView(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	uid := strconv.FormatInt(u.ID, 10)
	kb := &Keyboard{InlineKeyboard: [][]Button{
		{{Text: "🔑", CallbackData: "ab:ux:k:" + uid},
			{Text: w.tariffVerb, CallbackData: "ab:ux:p:" + uid},
			{Text: w.limitPick, CallbackData: "ab:ux:l:" + uid}},
		{{Text: w.expPick, CallbackData: "ab:ux:e:" + uid},
			{Text: w.inbPick, CallbackData: "ab:ux:i:" + uid}},
		{{Text: "TG", CallbackData: "ab:ux:y:" + uid},
			{Text: "📷", CallbackData: "ab:xv:" + uid},
			{Text: "📝", CallbackData: "ab:xnote:" + uid}},
		{{Text: w.reissueVerb, CallbackData: "ab:cf:reissue:" + uid},
			{Text: w.deleteVerb, CallbackData: "ab:cf:delete:" + uid}},
		{{Text: w.back, CallbackData: "ab:user:" + uid}},
	}}
	b.say(out, chat, userCardTitle(w, u), kb)
}

func userCardTitle(w adminWords, u db.User) string {
	return render(w.userTitle, map[string]string{"id": strconv.FormatInt(u.ID, 10), "name": u.Name})
}

// goBack edits nothing: a fresh card arrives as a new message, like elsewhere.
func (b *AdminBot) subBack(out *Outbox, w adminWords, chat int64, uid string) *Keyboard {
	return &Keyboard{InlineKeyboard: [][]Button{
		{{Text: w.back, CallbackData: "ab:user:" + uid}},
	}}
}

// trafficView: today, the week bars and the all-time totals.
func (b *AdminBot) trafficView(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", false) {
		return
	}
	uid := strconv.FormatInt(u.ID, 10)
	text := userCardTitle(w, u) + "\n" + b.trafficText(ctx, w, u.ID) +
		"\nΣ ↑" + fmtBytes(u.TotalUp) + " ↓" + fmtBytes(u.TotalDown)
	kb := b.subBack(out, w, chat, uid)
	b.say(out, chat, text, kb)
}

func fmtDateTime(unix int64) string {
	if unix <= 0 {
		return "—"
	}
	return time.Unix(unix, 0).UTC().Format("02.01 15:04")
}

// devicesView: connection addresses and bound devices.
func (b *AdminBot) devicesView(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", false) {
		return
	}
	uid := strconv.FormatInt(u.ID, 10)
	var lines []string
	lines = append(lines, userCardTitle(w, u))
	if addrs, err := b.d.Store.Q.ListUserDevices(ctx, u.ID); err == nil {
		for i, d := range addrs {
			if i >= 8 {
				break
			}
			st := fmtDateTime(d.LastSeen)
			if b.deviceLive(ctx, u.ID, d.Ip) {
				st = "online"
			}
			lines = append(lines, "🌐 "+maskIP(d.Ip)+" · "+st)
		}
	}
	if devs, err := b.d.Store.Q.ListBoundDevices(ctx, u.ID); err == nil {
		used := map[int64]int64{}
		if rows, err := b.d.Store.Q.ListBoundDeviceTraffic(ctx, u.ID); err == nil {
			for _, r := range rows {
				used[r.DeviceID] = r.Up + r.Down
			}
		}
		for _, d := range devs {
			st := fmtDateTime(d.LastSeen)
			if b.boundLive(ctx, d.SlotID) {
				st = "online"
			}
			line := "📱 " + esc(boundLabel(d.Name, d.Hwid, d.Os, d.OsVersion, d.Model, d.App, d.ID)) + " · " + st
			if n := used[d.ID]; n > 0 {
				line += " · " + fmtBytes(n)
			}
			lines = append(lines, line)
		}
	}
	kb := &Keyboard{InlineKeyboard: [][]Button{
		{{Text: w.unbindVerb, CallbackData: "ab:unb:" + uid}},
		{{Text: w.banVerb, CallbackData: "ab:ban:" + uid}},
	}}
	if bans, err := b.d.Store.Q.ListDeviceBans(ctx, u.ID); err == nil && len(bans) > 0 {
		for _, bn := range bans {
			label := bn.Label
			if label == "" {
				label = bn.Hwid
			}
			lines = append(lines, "🚫 "+esc(label))
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{Text: w.unbansVerb + " (" + strconv.Itoa(len(bans)) + ")", CallbackData: "ab:unbans:" + uid}})
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{Text: w.back, CallbackData: "ab:user:" + uid}})
	b.say(out, chat, strings.Join(lines, "\n"), kb)
}

// deviceLive says whether the exact IP is live on any of the user's slots.
func (b *AdminBot) deviceLive(ctx context.Context, userID int64, ip string) bool {
	if b.d.Online == nil {
		return false
	}
	slots, err := b.d.Store.Q.ListUserSlots(ctx, userID)
	if err != nil {
		return false
	}
	live := b.d.Online()
	for _, s := range slots {
		for _, lip := range live[s].IPs {
			if lip == ip {
				return true
			}
		}
	}
	return false
}

// boundLive says whether the device's own slot has live traffic.
func (b *AdminBot) boundLive(ctx context.Context, slotID int64) bool {
	if b.d.Online == nil {
		return false
	}
	slot, err := b.d.Store.Q.GetSlot(ctx, slotID)
	if err != nil || slot.Name == "" {
		return false
	}
	return len(b.d.Online()[slot.Name].IPs) > 0
}

// keysHub lists key names and offers the secrets behind a warning tap.
func (b *AdminBot) keysHub(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", false) {
		return
	}
	uid := strconv.FormatInt(u.ID, 10)
	links, _, err := b.userLinks(ctx, u)
	if err != nil || len(links) == 0 {
		b.say(out, chat, w.noKeys, b.subBack(out, w, chat, uid))
		return
	}
	var lines []string
	lines = append(lines, "<b>"+esc(w.keysTitle)+"</b>")
	for _, l := range links {
		lines = append(lines, "🔑 "+esc(l.Name))
	}
	kb := &Keyboard{InlineKeyboard: [][]Button{
		{{Text: w.linkTitle, CallbackData: "ab:xw:" + uid},
			{Text: w.keysTitle, CallbackData: "ab:xk:" + uid}},
		{{Text: w.back, CallbackData: "ab:user:" + uid}},
	}}
	b.say(out, chat, strings.Join(lines, "\n")+"\n\n"+w.keysWarn, kb)
}

// userLinks builds the subscription link and per-key links of the main slot.
func (b *AdminBot) userLinks(ctx context.Context, u db.User) ([]subs.Link, string, error) {
	var links []subs.Link
	if b.d.SubBase == nil || b.d.SubConfig == nil {
		return nil, "", errAdminNoGrant
	}
	base := b.d.SubBase(ctx)
	if base == "" || !u.SlotID.Valid {
		return nil, "", errAdminNoGrant
	}
	subURL := base + "/" + u.SubToken
	cfg, err := b.d.SubConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	slot, err := b.d.Store.Q.GetSlot(ctx, u.SlotID.Int64)
	if err != nil {
		return nil, "", err
	}
	prof, err := subs.ProfileForUser(ctx, b.d.Store.Q, b.d.Now, cfg, u, slot)
	if err != nil {
		return nil, "", err
	}
	links, err = subs.Links(prof)
	if err != nil {
		return nil, "", err
	}
	return links, subURL, nil
}

// revealLink sends the subscription link after the hub warned.
func (b *AdminBot) revealLink(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", false) {
		return
	}
	_, subURL, err := b.userLinks(ctx, u)
	if err != nil || subURL == "" {
		b.say(out, chat, w.noKeys, nil)
		return
	}
	b.safeAudit(ctx, admin.ID, "adminbot.link_shown", "user", strconv.FormatInt(u.ID, 10), nil)
	uid := strconv.FormatInt(u.ID, 10)
	kb := &Keyboard{InlineKeyboard: [][]Button{{{Text: w.back, CallbackData: "ab:user:" + uid}}}}
	b.say(out, chat, "<b>"+esc(w.linkTitle)+"</b>\n<code>"+esc(subURL)+"</code>", kb)
}

// revealKeys sends every key after the hub warned.
func (b *AdminBot) revealKeys(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", false) {
		return
	}
	links, _, err := b.userLinks(ctx, u)
	if err != nil || len(links) == 0 {
		b.say(out, chat, w.noKeys, nil)
		return
	}
	b.safeAudit(ctx, admin.ID, "adminbot.keys_shown", "user", strconv.FormatInt(u.ID, 10), map[string]any{"n": len(links)})
	var parts []string
	parts = append(parts, "<b>"+esc(w.keysTitle)+"</b>")
	n := 0
	for _, l := range links {
		if n >= 10 {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, "🔑 "+esc(l.Name)+"\n<code>"+esc(l.URI)+"</code>")
		n++
	}
	uid := strconv.FormatInt(u.ID, 10)
	kb := &Keyboard{InlineKeyboard: [][]Button{{{Text: w.back, CallbackData: "ab:user:" + uid}}}}
	b.say(out, chat, strings.Join(parts, "\n"), kb)
}

// avatarPhoto sends the user's picture, if any.
func (b *AdminBot) avatarPhoto(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", false) {
		return
	}
	if b.d.DataDir == "" {
		b.say(out, chat, w.avatarNone, nil)
		return
	}
	avatar, err := b.avatarFile(ctx, u.ID)
	if err != nil || avatar == "" {
		b.say(out, chat, w.avatarNone, nil)
		return
	}
	raw, err := os.ReadFile(avatar)
	if err != nil {
		b.say(out, chat, w.avatarNone, nil)
		return
	}
	uid := strconv.FormatInt(u.ID, 10)
	out.Reply(chat, "ab:xv:"+uid, 1, func(ctx context.Context, c *Client) error {
		_, err := c.SendPhoto(ctx, chat, "#"+uid+" "+esc(u.Name), raw)
		return err
	})
}

// avatarFile resolves the stored avatar inside the data dir.
func (b *AdminBot) avatarFile(ctx context.Context, userID int64) (string, error) {
	var name string
	row := b.d.Store.DB.QueryRowContext(ctx, `SELECT avatar FROM users WHERE id = ?`, userID)
	var ns sql.NullString
	if err := row.Scan(&ns); err != nil {
		return "", err
	}
	if ns.Valid {
		name = ns.String
	}
	if name == "" || name != filepath.Base(name) {
		return "", nil
	}
	return filepath.Join(b.d.DataDir, "avatars", name), nil
}

// poolsView lists pool limits and traffic packages with remains.
func (b *AdminBot) poolsView(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", false) {
		return
	}
	uid := strconv.FormatInt(u.ID, 10)
	var lines []string
	lines = append(lines, "<b>"+esc(w.poolsTitle)+"</b>")
	if ps, err := b.d.Store.Q.ListTrafficPools(ctx); err == nil && len(ps) > 0 {
		rows, _ := b.d.Store.Q.ListUserPools(ctx, u.ID)
		mine := map[int64]db.UserPool{}
		for _, r := range rows {
			mine[r.PoolID] = r
		}
		grants, _ := domain.UserGrantsLeft(ctx, b.d.Store.Q, u.ID, b.d.Now())
		for _, p := range ps {
			r := mine[p.ID]
			line := "🪣 " + esc(p.Name) + ": " + fmtBytes(r.UsedUp+r.UsedDown)
			if r.TrafficLimit.Valid {
				line += " / " + fmtBytes(r.TrafficLimit.Int64)
			} else {
				line += " / ∞"
			}
			if grants != nil {
				line += " (+" + fmtBytes(grants.Pool(u.ID, p.ID)) + ")"
			}
			if domain.PoolExhausted(r, 0) {
				line += " 🧱"
			}
			lines = append(lines, line)
		}
	}
	if gs, err := b.d.Store.Q.ListUserGrants(ctx, u.ID); err == nil && len(gs) > 0 {
		names := map[int64]string{}
		if ps, err := b.d.Store.Q.ListAllTrafficPackages(ctx); err == nil {
			for _, p := range ps {
				names[p.ID] = p.Name
			}
		}
		lines = append(lines, "<b>"+esc(w.grantsTitle)+"</b>")
		for _, g := range gs {
			nm := names[g.PackageID.Int64]
			if nm == "" {
				nm = "#" + strconv.FormatInt(g.ID, 10)
			}
			active := ""
			if !domain.GrantActive(g, b.d.Now()) {
				active = " ⌛"
			}
			note := ""
			if strings.TrimSpace(g.Note) != "" {
				note = " — " + g.Note
			}
			lines = append(lines, "🎟 "+esc(nm)+": "+fmtBytes(g.Remaining)+active+esc(note))
		}
	}
	kb := &Keyboard{InlineKeyboard: [][]Button{{{Text: w.back, CallbackData: "ab:user:" + uid}}}}
	b.say(out, chat, strings.Join(lines, "\n"), kb)
}

// tariffView lists active tariffs for a switch (confirmed like the panel).
func (b *AdminBot) tariffView(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", true) {
		return
	}
	ts, err := b.d.Store.Q.ListTariffs(ctx)
	if err != nil {
		return
	}
	uid := strconv.FormatInt(u.ID, 10)
	kb := &Keyboard{}
	for _, t := range ts {
		if t.Archived != 0 {
			continue
		}
		label := t.Name
		if strings.TrimSpace(t.PriceLabel) != "" {
			label += " · " + t.PriceLabel
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{
			Text:         label,
			CallbackData: "ab:cf:tariff:" + uid + ":" + strconv.FormatInt(t.ID, 10),
		}})
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{Text: w.back, CallbackData: "ab:user:" + uid}})
	b.say(out, chat, w.tariffPick, kb)
}

// limitView sets the device limit outright, like the panel's segmented control.
func (b *AdminBot) limitView(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", true) {
		return
	}
	uid := strconv.FormatInt(u.ID, 10)
	kb := &Keyboard{InlineKeyboard: [][]Button{}}
	var row []Button
	for _, n := range []string{"1", "2", "3", "5", "10", "∞"} {
		v := n
		if n == "∞" {
			v = "inf"
		}
		row = append(row, Button{Text: n, CallbackData: "ab:xl:" + uid + ":" + v})
		if len(row) == 3 {
			kb.InlineKeyboard = append(kb.InlineKeyboard, row)
			row = nil
		}
	}
	if len(row) > 0 {
		kb.InlineKeyboard = append(kb.InlineKeyboard, row)
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{Text: w.back, CallbackData: "ab:user:" + uid}})
	b.say(out, chat, w.limitPick, kb)
}

// inboundsView toggles allowed inbounds; switching the last one off is refused.
func (b *AdminBot) inboundsView(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", true) {
		return
	}
	ins, err := b.d.Store.Q.ListInbounds(ctx)
	if err != nil || len(ins) == 0 {
		return
	}
	uid := strconv.FormatInt(u.ID, 10)
	all := len(domain.DecodeInbounds(u.Inbounds)) == 0
	kb := &Keyboard{}
	for _, in := range ins {
		on := all
		if !all {
			for _, id := range domain.DecodeInbounds(u.Inbounds) {
				if id == in.ID {
					on = true
				}
			}
		}
		mark := "❌"
		if on {
			mark = "✅"
		}
		name := in.Name
		if name == "" {
			name = in.DisplayName
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{
			Text:         mark + " " + name,
			CallbackData: "ab:xi:" + uid + ":" + strconv.FormatInt(in.ID, 10),
		}})
	}
	kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{Text: w.back, CallbackData: "ab:user:" + uid}})
	b.say(out, chat, w.inbPick, kb)
}

// tgView shows the linked Telegram and offers an unlink.
func (b *AdminBot) tgView(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, u db.User) {
	if !b.check(out, w, chat, admin, "users", false) {
		return
	}
	uid := strconv.FormatInt(u.ID, 10)
	link, err := b.d.Store.Q.TgLinkOfUser(ctx, u.ID)
	if err != nil {
		b.say(out, chat, w.noTg, b.subBack(out, w, chat, uid))
		return
	}
	who := "@" + link.Username
	if link.Username == "" {
		who = link.FirstName
	}
	kb := &Keyboard{InlineKeyboard: [][]Button{
		{{Text: w.tgunlinkVerb, CallbackData: "ab:cf:tgunlink:" + uid}},
		{{Text: w.back, CallbackData: "ab:user:" + uid}},
	}}
	b.say(out, chat, "✈️ "+esc(who), kb)
}

// doExtendDays extends by days outright and reports.
func (b *AdminBot) doExtendDays(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, uid, days int64) {
	if !b.check(out, w, chat, admin, "users", true) {
		return
	}
	if _, err := b.d.Users.Extend(ctx, uid, days); err != nil {
		return
	}
	b.safeAudit(ctx, admin.ID, "adminbot.user_extend", "user", strconv.FormatInt(uid, 10), map[string]any{"days": days})
	b.say(out, chat, w.actExtended, nil)
}

// doExtendMonths extends by billing months.
func (b *AdminBot) doExtendMonths(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, uid, m int64) {
	if !b.check(out, w, chat, admin, "users", true) {
		return
	}
	if _, err := b.d.Users.ExtendMonths(ctx, uid, int(m)); err != nil {
		return
	}
	b.safeAudit(ctx, admin.ID, "adminbot.user_extend", "user", strconv.FormatInt(uid, 10), map[string]any{"months": m})
	b.say(out, chat, w.actExtended, nil)
}

// doLimit sets the device limit outright, like the panel's segmented control.
func (b *AdminBot) doLimit(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, uid int64, v string) {
	if !b.check(out, w, chat, admin, "users", true) {
		return
	}
	var p domain.Patch
	if v == "inf" {
		p.ClearDeviceLimit = true
	} else {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 || n > 1000 {
			return
		}
		p.DeviceLimit = &n
	}
	if _, err := b.d.Users.Update(ctx, uid, p); err != nil {
		return
	}
	b.safeAudit(ctx, admin.ID, "adminbot.user_limit", "user", strconv.FormatInt(uid, 10), map[string]any{"limit": v})
	b.say(out, chat, w.applied, nil)
}

// doToggleInbound flips one inbound, refusing to switch the last one off.
func (b *AdminBot) doToggleInbound(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, uid, inbid int64) {
	if !b.check(out, w, chat, admin, "users", true) {
		return
	}
	u, err := b.d.Users.Get(ctx, uid)
	if err != nil {
		return
	}
	cur := domain.DecodeInbounds(u.Inbounds)
	all := len(cur) == 0
	var next []int64
	if all {
		ins, err := b.d.Store.Q.ListInbounds(ctx)
		if err != nil {
			return
		}
		for _, in := range ins {
			if in.ID != inbid {
				next = append(next, in.ID)
			}
		}
	} else {
		off := false
		for _, id := range cur {
			if id == inbid {
				off = true
				continue
			}
			next = append(next, id)
		}
		if !off {
			next = append(cur, inbid)
		}
	}
	if len(next) == 0 {
		b.say(out, chat, w.inbEmpty, nil)
		return
	}
	if _, err := b.d.Users.Update(ctx, uid, domain.Patch{Inbounds: &next}); err != nil {
		return
	}
	b.safeAudit(ctx, admin.ID, "adminbot.user_inbounds", "user", strconv.FormatInt(uid, 10), map[string]any{"inbound": inbid})
	if fresh, err := b.d.Users.Get(ctx, uid); err == nil {
		b.inboundsView(ctx, out, w, chat, admin, fresh)
	}
}

// consumeAwait answers a pending free-text question.
func (b *AdminBot) consumeAwait(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, kind, arg, text string) {
	switch kind {
	case "note", "contact":
		if !b.check(out, w, chat, admin, "users", true) {
			return
		}
		uid, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return
		}
		var p domain.Patch
		if kind == "note" {
			p.Note = &text
		} else {
			p.Contact = &text
		}
		if _, err := b.d.Users.Update(ctx, uid, p); err != nil {
			return
		}
		b.safeAudit(ctx, admin.ID, "adminbot.user_edit", "user", arg, map[string]any{"field": kind})
		b.say(out, chat, w.applied, nil)
	case "expdate":
		if !b.check(out, w, chat, admin, "users", true) {
			return
		}
		uid, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return
		}
		at, err := time.Parse("02.01.2006", strings.TrimSpace(text))
		if err != nil {
			b.say(out, chat, w.dateBad, nil)
			return
		}
		if _, err := b.d.Users.Update(ctx, uid, domain.Patch{ExpiresAt: &at}); err != nil {
			return
		}
		b.safeAudit(ctx, admin.ID, "adminbot.user_expiry", "user", arg, map[string]any{"date": text})
		b.say(out, chat, w.applied, nil)
	case "billday":
		if !b.check(out, w, chat, admin, "users", true) {
			return
		}
		uid, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return
		}
		n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil || n < 0 || n > 31 {
			b.say(out, chat, w.billBad, nil)
			return
		}
		var p domain.Patch
		if n == 0 {
			p.ClearBillingDay = true
		} else {
			p.BillingDay = &n
		}
		if _, err := b.d.Users.Update(ctx, uid, p); err != nil {
			return
		}
		b.safeAudit(ctx, admin.ID, "adminbot.user_billing", "user", arg, map[string]any{"day": n})
		b.say(out, chat, w.applied, nil)
	case "newname":
		if !b.check(out, w, chat, admin, "users", true) {
			return
		}
		name := strings.TrimSpace(text)
		if name == "" {
			b.say(out, chat, w.newAskName, nil)
			return
		}
		b.mu.Lock()
		b.creating[chat] = createState{name: truncateRunes(name, 64), at: b.d.Now()}
		b.mu.Unlock()
		b.awaitAsk(chat, "newcontact")
		b.say(out, chat, w.newAskContact, nil)
	case "newcontact":
		if !b.check(out, w, chat, admin, "users", true) {
			return
		}
		b.mu.Lock()
		st, ok := b.creating[chat]
		b.mu.Unlock()
		if !ok || b.d.Now().Sub(st.at) > 15*time.Minute {
			b.say(out, chat, w.newAskName, nil)
			b.awaitAsk(chat, "newname")
			return
		}
		contact := strings.TrimSpace(text)
		if contact == "-" {
			contact = ""
		}
		st.contact = truncateRunes(contact, 200)
		b.mu.Lock()
		b.creating[chat] = st
		b.mu.Unlock()
		b.tariffPick(ctx, out, w, chat, admin)
	}
}

// tariffPick lists active tariffs for a pending create.
func (b *AdminBot) tariffPick(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin) {
	ts, err := b.d.Store.Q.ListTariffs(ctx)
	if err != nil {
		return
	}
	kb := &Keyboard{}
	for _, t := range ts {
		if t.Archived != 0 {
			continue
		}
		label := t.Name
		if strings.TrimSpace(t.PriceLabel) != "" {
			label += " · " + t.PriceLabel
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, []Button{{
			Text:         label,
			CallbackData: "ab:xn:" + strconv.FormatInt(t.ID, 10),
		}})
	}
	b.say(out, chat, w.newAskTariff, kb)
}

// createFromStash finishes /new once the tariff is tapped.
func (b *AdminBot) createFromStash(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, tid int64) {
	if !b.check(out, w, chat, admin, "users", true) {
		return
	}
	b.mu.Lock()
	st, ok := b.creating[chat]
	if ok {
		delete(b.creating, chat)
	}
	b.mu.Unlock()
	if !ok || b.d.Now().Sub(st.at) > 15*time.Minute || st.name == "" {
		b.say(out, chat, w.newAskName, nil)
		b.awaitAsk(chat, "newname")
		return
	}
	u, err := b.d.Users.Create(ctx, domain.CreateInput{Name: st.name, Contact: st.contact, TariffID: tid})
	if err != nil {
		b.say(out, chat, w.userNotFound, nil)
		return
	}
	b.safeAudit(ctx, admin.ID, "adminbot.user_create", "user", strconv.FormatInt(u.ID, 10), map[string]any{"tariff": tid})
	b.say(out, chat, w.userCreated, nil)
	b.showUser(ctx, nil, out, w, chat, admin, u, nil)
}

// avatarNameOf reads the stored avatar file name, "" for none.
func (b *AdminBot) avatarNameOf(ctx context.Context, userID int64) string {
	var s sql.NullString
	if err := b.d.Store.DB.QueryRowContext(ctx, `SELECT avatar FROM users WHERE id = ?`, userID).Scan(&s); err != nil || !s.Valid {
		return ""
	}
	return s.String
}

// dropAvatarFile removes a stored avatar file; hostile names never escape.
func (b *AdminBot) dropAvatarFile(name string) {
	if b.d.DataDir == "" || name == "" || name != filepath.Base(name) {
		return
	}
	_ = os.Remove(filepath.Join(b.d.DataDir, "avatars", name))
}

// userSection routes the card's sub-views.
func (b *AdminBot) userSection(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, chat int64, admin db.Admin, kind, uidStr string) {
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		return
	}
	u, err := b.d.Users.Get(ctx, uid)
	if err != nil {
		b.answer(c, qID(q), w.userNotFound)
		return
	}
	switch kind {
	case "t":
		b.trafficView(ctx, out, w, chat, admin, u)
	case "d":
		b.devicesView(ctx, out, w, chat, admin, u)
	case "k":
		b.keysHub(ctx, out, w, chat, admin, u)
	case "e":
		b.extendView(ctx, out, w, chat, admin, uidStr)
	case "p":
		b.tariffView(ctx, out, w, chat, admin, u)
	case "l":
		b.limitView(ctx, out, w, chat, admin, u)
	case "i":
		b.inboundsView(ctx, out, w, chat, admin, u)
	case "g":
		b.poolsView(ctx, out, w, chat, admin, u)
	case "m":
		b.moreView(ctx, out, w, chat, admin, u)
	case "y":
		b.tgView(ctx, out, w, chat, admin, u)
	default:
		b.answer(c, qID(q), w.unknown)
	}
}

func qID(q *CallbackQuery) string {
	if q == nil {
		return ""
	}
	return q.ID
}

// extendView picks a term: days, billing months, an exact date or forever.
func (b *AdminBot) extendView(ctx context.Context, out *Outbox, w adminWords, chat int64, admin db.Admin, uidStr string) {
	if !b.check(out, w, chat, admin, "users", true) {
		return
	}
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		return
	}
	u, err := b.d.Users.Get(ctx, uid)
	if err != nil {
		return
	}
	kb := &Keyboard{InlineKeyboard: [][]Button{
		{{Text: "+7", CallbackData: "ab:x:" + uidStr + ":7"},
			{Text: "+30", CallbackData: "ab:x:" + uidStr + ":30"},
			{Text: "+90", CallbackData: "ab:x:" + uidStr + ":90"},
			{Text: "+365", CallbackData: "ab:x:" + uidStr + ":365"}},
		{{Text: w.dateAsk, CallbackData: "ab:xdate:" + uidStr},
			{Text: w.billAsk, CallbackData: "ab:xbill:" + uidStr}},
		{{Text: w.foreverVerb, CallbackData: "ab:cf:forever:" + uidStr},
			{Text: w.back, CallbackData: "ab:user:" + uidStr}},
	}}
	if u.BillingDay.Valid {
		kb.InlineKeyboard = append([][]Button{{
			{Text: "+1 мес", CallbackData: "ab:xm:" + uidStr + ":1"},
			{Text: "+3", CallbackData: "ab:xm:" + uidStr + ":3"},
			{Text: "+6", CallbackData: "ab:xm:" + uidStr + ":6"},
			{Text: "+12", CallbackData: "ab:xm:" + uidStr + ":12"},
		}}, kb.InlineKeyboard...)
	}
	b.say(out, chat, w.expPick+" ("+fmtDate(u.ExpiresAt.Int64)+")", kb)
	_ = uid
}

// revealLinkTap / revealKeysTap resolve the user and reveal secrets.
func (b *AdminBot) revealLinkTap(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, chat int64, admin db.Admin, uidStr string) {
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		return
	}
	u, err := b.d.Users.Get(ctx, uid)
	if err != nil {
		b.answer(c, q.ID, w.userNotFound)
		return
	}
	b.revealLink(ctx, out, w, chat, admin, u)
}

func (b *AdminBot) revealKeysTap(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, chat int64, admin db.Admin, uidStr string) {
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		return
	}
	u, err := b.d.Users.Get(ctx, uid)
	if err != nil {
		b.answer(c, q.ID, w.userNotFound)
		return
	}
	b.revealKeys(ctx, out, w, chat, admin, u)
}

// avatarTap sends the user's picture.
func (b *AdminBot) avatarTap(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, chat int64, admin db.Admin, uidStr string) {
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		return
	}
	u, err := b.d.Users.Get(ctx, uid)
	if err != nil {
		b.answer(c, q.ID, w.userNotFound)
		return
	}
	b.avatarPhoto(ctx, out, w, chat, admin, u)
}

// askFreeText arms a one-line question (note, contact, date, bill day).
func (b *AdminBot) askFreeText(ctx context.Context, c *Client, out *Outbox, w adminWords, q *CallbackQuery, chat int64, admin db.Admin, kind, ask string) {
	if !b.check(out, w, chat, admin, "users", true) {
		b.answer(c, q.ID, w.noAccess)
		return
	}
	b.awaitAsk(chat, kind)
	b.say(out, chat, ask, nil)
}
