package tgbot

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"html"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cozy/internal/panel/domain"
	"cozy/internal/panel/nodeapi"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
	"cozy/internal/panel/store/db"
)

// The admin bot is a second, separate bot with its own token: the panel in
// Telegram for the owner and the sub-admins. The subscriber bot is untouched;
// this file reuses its transport (Client), sending lanes (Outbox) and route,
// but has its own settings keys, chats table, link codes and command router.

// Settings keys of the admin bot. The token never leaves the panel's API.
const (
	KeyAdminEnabled = "tg_admin_enabled"
	KeyAdminToken   = "tg_admin_token" // never leaves the panel's API
	KeyAdminBot     = "tg_admin_bot"   // the token's bot, from getMe
	KeyAdminSecret  = "tg_admin_secret"
	KeyAdminOffset  = "tg_admin_offset" // the last update taken, per bot
	KeyAdminRoute   = "telegram.admin_route"
)

// AdminEnabled switches the admin bot on; off until the owner connects it.
var AdminEnabled = settings.Switch{Key: KeyAdminEnabled}

// AdminDeps is what the admin bot needs from the panel.
type AdminDeps struct {
	Store    *store.Store
	Settings *settings.Settings
	Users    *domain.Users
	Devices  *domain.Devices
	// Online is who is online by slot name; nil when the panel runs without nodes.
	Online func() map[string]nodeapi.Online
	// Health is a node's health; nil when the panel runs without nodes.
	Health func(id int64) (bool, string)
	// MiniApp: the panel's certificate is one Telegram apps accept (not self-signed).
	MiniApp func() bool
	// AdminBase is https://host:port/<admin path> for the Mini App button, ""
	// while the panel has no public address.
	AdminBase func(ctx context.Context) string
	API     string // the Bot API; DefaultAPI unless testing
	Log     interface {
		Warn(msg string, args ...any)
		Error(msg string, args ...any)
	}
	Now    func() time.Time
	Limits Limits // zero: DefaultLimits
	// Tunnel reaches addr through node id, for RouteNode; nil: the panel has no nodes.
	Tunnel func(ctx context.Context, nodeID int64, addr string) (net.Conn, error)
}

// AdminBot is the panel for admins in Telegram.
type AdminBot struct {
	d      AdminDeps
	reload chan struct{}
	out    atomic.Pointer[Outbox]
	client atomic.Pointer[Client]

	mu     sync.Mutex
	status Status
	input  map[int64][]time.Time
	bcast  BroadcastProgress
	// Link requests of sub-admins waiting for the owner's approval.
	requests map[int64]adminRequest
	// Pending broadcasts: chat → text waiting for the owner's confirm tap.
	pending map[int64]string

	running sync.WaitGroup
}

type adminRequest struct {
	username  string
	firstName string
	login     string // the admin login the chat claims
	at        time.Time
}

// requestTTL: a link request the owner never answers dies on its own.
const requestTTL = time.Hour

func NewAdminBot(d AdminDeps) *AdminBot {
	if d.API == "" {
		d.API = DefaultAPI
	}
	if d.Limits == (Limits{}) {
		d.Limits = DefaultLimits
	}
	return &AdminBot{d: d, reload: make(chan struct{}, 1), input: map[int64][]time.Time{}, requests: map[int64]adminRequest{}, pending: map[int64]string{}}
}

// Status of the running admin bot.
func (b *AdminBot) AdminStatus() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

func (b *AdminBot) setAdminStatus(fn func(*Status)) {
	b.mu.Lock()
	fn(&b.status)
	b.mu.Unlock()
}

// Reload restarts the admin bot with the settings as they are now.
func (b *AdminBot) Reload() {
	select {
	case b.reload <- struct{}{}:
	default:
	}
}

// tz returns the panel language for bot texts.
func (b *AdminBot) lang(ctx context.Context) string {
	lang, _ := b.d.Settings.Lang(ctx)
	if lang != "ru" && lang != "en" {
		return "ru"
	}
	return lang
}

// --- chats table (raw SQL: the table is new in 0021 and small) ---

type adminChat struct {
	TgID      int64
	AdminID   int64
	Username  string
	FirstName string
	Blocked   int64
	CreatedAt int64
}

func (b *AdminBot) upsertAdminChat(ctx context.Context, tgID, adminID int64, username, firstName string, now int64) error {
	_, err := b.d.Store.DB.ExecContext(ctx, `INSERT INTO tg_admin_chats (tg_id, admin_id, username, first_name, blocked, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?)
ON CONFLICT (tg_id) DO UPDATE SET admin_id = excluded.admin_id, username = excluded.username, first_name = excluded.first_name, blocked = 0, updated_at = excluded.updated_at`, tgID, adminID, username, firstName, now, now)
	return err
}

func (b *AdminBot) getAdminChat(ctx context.Context, tgID int64) (adminChat, error) {
	var c adminChat
	err := b.d.Store.DB.QueryRowContext(ctx, `SELECT tg_id, admin_id, username, first_name, blocked, created_at FROM tg_admin_chats WHERE tg_id = ?`, tgID).
		Scan(&c.TgID, &c.AdminID, &c.Username, &c.FirstName, &c.Blocked, &c.CreatedAt)
	if err != nil {
		return adminChat{}, err
	}
	return c, nil
}

func (b *AdminBot) dropAdminChat(ctx context.Context, tgID int64) error {
	_, err := b.d.Store.DB.ExecContext(ctx, `DELETE FROM tg_admin_chats WHERE tg_id = ?`, tgID)
	return err
}

func (b *AdminBot) dropAdminChatsOf(ctx context.Context, adminID int64) error {
	_, err := b.d.Store.DB.ExecContext(ctx, `DELETE FROM tg_admin_chats WHERE admin_id = ?`, adminID)
	return err
}

func (b *AdminBot) setAdminChatBlocked(ctx context.Context, tgID int64) error {
	_, err := b.d.Store.DB.ExecContext(ctx, `UPDATE tg_admin_chats SET blocked = 1 WHERE tg_id = ?`, tgID)
	return err
}

func (b *AdminBot) listAdminChats(ctx context.Context) ([]adminChat, error) {
	rows, err := b.d.Store.DB.QueryContext(ctx, `SELECT tg_id, admin_id, username, first_name, blocked, created_at FROM tg_admin_chats ORDER BY created_at, tg_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adminChat
	for rows.Next() {
		var c adminChat
		if err := rows.Scan(&c.TgID, &c.AdminID, &c.Username, &c.FirstName, &c.Blocked, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (b *AdminBot) adminTargets(ctx context.Context) ([]int64, error) {
	rows, err := b.d.Store.DB.QueryContext(ctx, `SELECT tg_id FROM tg_admin_chats WHERE COALESCE(blocked, 0) = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ownerChat is the linked chat of the owner, 0 when the owner linked nothing.
func (b *AdminBot) ownerChat(ctx context.Context) int64 {
	var tgID sql.NullInt64
	_ = b.d.Store.DB.QueryRowContext(ctx, `SELECT c.tg_id FROM tg_admin_chats c JOIN admins a ON a.id = c.admin_id WHERE a.is_owner != 0 LIMIT 1`).Scan(&tgID)
	if !tgID.Valid {
		return 0
	}
	return tgID.Int64
}

// --- link codes (own HMAC label: a subscriber code must never open the admin bot) ---

// LinkTTL: a code the owner never uses dies on its own.
const adminLinkTTL = 30 * time.Minute

func adminSum(secret, b []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("cozy-tg-admin"))
	m.Write(b)
	return m.Sum(nil)[:macSize]
}

// AdminLinkCode is a code for one admin; the owner takes it from the panel.
func AdminLinkCode(secret []byte, adminID int64, now time.Time) string {
	b := make([]byte, 16, 16+macSize)
	binary.BigEndian.PutUint64(b, uint64(adminID))
	binary.BigEndian.PutUint64(b[8:], uint64(now.Add(adminLinkTTL).Unix()))
	return base64.RawURLEncoding.EncodeToString(append(b, adminSum(secret, b)...))
}

// ParseAdminLinkCode returns the admin a code was made for.
func ParseAdminLinkCode(secret []byte, code string, now time.Time) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil || len(raw) != 16+macSize || !hmac.Equal(raw[16:], adminSum(secret, raw[:16])) {
		return 0, ErrLinkInvalid
	}
	if now.Unix() > int64(binary.BigEndian.Uint64(raw[8:16])) {
		return 0, ErrLinkExpired
	}
	return int64(binary.BigEndian.Uint64(raw[:8])), nil
}

func (b *AdminBot) secret(ctx context.Context) ([]byte, error) {
	s, err := b.d.Settings.String(ctx, KeyAdminSecret)
	if err != nil {
		return nil, err
	}
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) == 32 {
		return raw, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return raw, settings.Set(ctx, b.d.Settings, KeyAdminSecret, base64.StdEncoding.EncodeToString(raw))
}

// LinkURL is the t.me link that opens the admin bot with a code for the admin;
// "" while the bot is off.
func (b *AdminBot) LinkURL(ctx context.Context, adminID int64) string {
	st := b.AdminStatus()
	if !st.Running || st.Bot.Username == "" {
		return ""
	}
	secret, err := b.secret(ctx)
	if err != nil {
		return ""
	}
	return "https://t.me/" + st.Bot.Username + "?start=" + AdminLinkCode(secret, adminID, b.d.Now())
}

// --- grants: who may do what (the matrix, read leniently) ---

var (
	errAdminNoGrant  = errNoGrant()
	errAdminClosed   = errClosed()
	errAdminNotFound = errGone()
)

func errNoGrant() error  { return errorString("no_grant") }
func errClosed() error   { return errorString("account_closed") }
func errGone() error     { return errorString("not_found") }

type errorString string

func (e errorString) Error() string { return string(e) }

// adminCan checks one bot action against the admin's matrix. The owner passes
// everything; managing admins (approve, unlink others, broadcast) is owner-only
// like the admins section. Unknown sections in stored scopes are ignored (not
// refused): a section added later must not lock the bot out.
func adminCan(a db.Admin, tag string, write bool) error {
	if a.DisabledAt.Valid {
		return errAdminClosed
	}
	if a.ExpiresAt.Valid && !time.Unix(a.ExpiresAt.Int64, 0).After(time.Now()) {
		return errAdminClosed
	}
	if a.IsOwner != 0 {
		return nil
	}
	if tag == "admins" {
		return errAdminNoGrant
	}
	var scopes map[string]string
	if strings.TrimSpace(a.Scopes) != "" {
		if err := json.Unmarshal([]byte(a.Scopes), &scopes); err != nil {
			return errAdminNoGrant
		}
	}
	got, ok := scopes[tag]
	if !ok {
		return errAdminNoGrant
	}
	if got != "r" && got != "w" {
		return errAdminNoGrant
	}
	if write && got != "w" {
		return errAdminNoGrant
	}
	return nil
}

// --- run loop ---

// Run keeps the admin bot polling while it is on.
func (b *AdminBot) Run(ctx context.Context) {
	var stop context.CancelFunc
	start := func() {
		if stop != nil {
			stop()
			stop = nil
		}
		on, _ := b.d.Settings.On(ctx, AdminEnabled)
		token, _ := b.d.Settings.String(ctx, KeyAdminToken)
		if !on || token == "" {
			b.setAdminStatus(func(s *Status) { *s = Status{} })
			return
		}
		rt, err := b.transport(b.adminRoute(ctx))
		if err != nil {
			b.setAdminStatus(func(s *Status) { *s = Status{Error: "route_invalid"} })
			return
		}
		pctx, cancel := context.WithCancel(ctx)
		stop = cancel
		c := NewClient(b.d.API, token, rt)
		b.running.Go(func() { b.poll(pctx, c) })
	}
	start()
	for {
		select {
		case <-ctx.Done():
			if stop != nil {
				stop()
			}
			b.running.Wait()
			return
		case <-b.reload:
			start()
		}
	}
}

func (b *AdminBot) poll(ctx context.Context, c *Client) {
	me, err := c.Me(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		b.setAdminStatus(func(s *Status) { *s = Status{Error: errText(err)} })
		b.retryLater(ctx)
		return
	}
	_ = settings.Set(ctx, b.d.Settings, KeyAdminBot, me)
	_ = c.DropWebhook(ctx)
	_ = c.SetCommands(ctx, b.words().commands)
	if url := b.miniAppURL(ctx); url != "" {
		_ = c.SetMenuButton(ctx, b.words().menuApp, url)
	} else {
		_ = c.SetMenuButton(ctx, "", "")
	}
	out := NewOutbox(c, b.d.Limits, time.Now, func(chat int64) {
		_ = b.setAdminChatBlocked(context.WithoutCancel(ctx), chat)
	}, func(err error) { b.d.Log.Warn("adminbot: send", "err", errText(err)) })
	b.running.Go(func() { out.Run(ctx) })
	b.out.Store(out)
	b.client.Store(c)
	defer b.client.CompareAndSwap(c, nil)
	defer b.out.CompareAndSwap(out, nil)
	b.setAdminStatus(func(s *Status) { *s = Status{Running: true, Bot: me} })
	offset := b.loadOffset(ctx, me.ID)
	fails := map[int64]int{}
	backoff := time.Second
	for ctx.Err() == nil {
		ups, err := c.Updates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var ae *APIError
			if errors.As(err, &ae) && ae.Code == 401 {
				b.setAdminStatus(func(s *Status) { *s = Status{Error: "token_revoked"} })
				return
			}
			b.setAdminStatus(func(s *Status) { s.Error = errText(err) })
			sleep(ctx, backoff)
			backoff = min(2*backoff, time.Minute)
			continue
		}
		backoff = time.Second
		b.setAdminStatus(func(s *Status) { s.Error = "" })
		for _, up := range ups {
			if err := b.handle(ctx, c, up); err != nil && ctx.Err() == nil {
				if fails[up.UpdateID]++; fails[up.UpdateID] < maxUpdateTries {
					sleep(ctx, time.Duration(fails[up.UpdateID])*updateRetryPause)
					break
				}
				b.d.Log.Error("adminbot: update given up", "update", up.UpdateID, "err", err)
			}
			if ctx.Err() != nil {
				return
			}
			delete(fails, up.UpdateID)
			offset = up.UpdateID + 1
			b.saveOffset(ctx, me.ID, offset)
		}
	}
}

func (b *AdminBot) retryLater(ctx context.Context) {
	b.running.Go(func() {
		sleep(ctx, time.Minute)
		if ctx.Err() == nil {
			b.Reload()
		}
	})
}

func (b *AdminBot) loadOffset(ctx context.Context, bot int64) int64 {
	rec, ok, err := settings.Get[offsetRecord](ctx, b.d.Settings, KeyAdminOffset)
	if err != nil || !ok || rec.Bot != bot {
		return 0
	}
	return rec.Offset
}

func (b *AdminBot) saveOffset(ctx context.Context, bot, offset int64) {
	raw, _ := json.Marshal(offsetRecord{Bot: bot, Offset: offset})
	if err := b.d.Store.Q.SetSetting(ctx, db.SetSettingParams{Key: KeyAdminOffset, Value: string(raw)}); err != nil && ctx.Err() == nil {
		b.d.Log.Warn("adminbot: offset not saved", "err", err)
	}
}

func (b *AdminBot) flooding(chat int64) bool {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	recent := b.input[chat][:0]
	for _, t := range b.input[chat] {
		if now.Sub(t) < floodWindow {
			recent = append(recent, t)
		}
	}
	b.input[chat] = append(recent, now)
	return len(recent) >= floodInputs
}

// --- route (own key, same shapes as the subscriber bot) ---

// LoadAdminRoute is the saved route. A read error is returned, not hidden.
func (b *AdminBot) LoadAdminRoute(ctx context.Context) (Route, error) {
	r, _, err := settings.Get[Route](ctx, b.d.Settings, KeyAdminRoute)
	if err != nil {
		return Route{}, err
	}
	if r.Mode == "" {
		r.Mode = RouteDirect
	}
	return r, nil
}

func (b *AdminBot) adminRoute(ctx context.Context) Route {
	r, err := b.LoadAdminRoute(ctx)
	if err != nil {
		return Route{Mode: RouteDirect}
	}
	return r
}

func (b *AdminBot) transport(r Route) (http.RoundTripper, error) {
	switch r.Mode {
	case RouteProxy:
		u, err := ParseProxy(r.Proxy)
		if err != nil {
			return nil, err
		}
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = http.ProxyURL(u)
		return t, nil
	case RouteNode:
		if b.d.Tunnel == nil || r.NodeID == 0 {
			return nil, ErrRouteNode
		}
		id := r.NodeID
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = nil
		t.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) { return b.d.Tunnel(ctx, id, addr) }
		return t, nil
	}
	return nil, nil
}

// CheckAdminRoute tries a route before it is saved.
func (b *AdminBot) CheckAdminRoute(ctx context.Context, r Route, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	rt, err := b.transport(r)
	if err != nil {
		return err
	}
	if token != "" {
		_, err := NewClient(b.d.API, token, rt).Me(ctx)
		var ae *APIError
		if err == nil || errors.As(err, &ae) {
			return nil
		}
		return ErrUnreachable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.d.API+"/", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Transport: rt}).Do(req)
	if err != nil {
		return ErrUnreachable
	}
	resp.Body.Close()
	return nil
}

// CheckAdminToken asks Telegram whose token this is, the way the bot goes there.
func (b *AdminBot) CheckAdminToken(ctx context.Context, token string) (User, error) {
	return b.CheckAdminTokenVia(ctx, token, b.adminRoute(ctx))
}

// CheckAdminTokenVia asks Telegram whose token this is, through route.
func (b *AdminBot) CheckAdminTokenVia(ctx context.Context, token string, route Route) (User, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	rt, err := b.transport(route)
	if err != nil {
		return User{}, err
	}
	return NewClient(b.d.API, token, rt).Me(ctx)
}

// --- broadcast to linked admins ---

// Broadcast queues text for every linked admin chat that did not block the bot.
func (b *AdminBot) Broadcast(ctx context.Context, text string) (int, error) {
	out := b.out.Load()
	if out == nil {
		return 0, ErrOff
	}
	b.mu.Lock()
	if b.bcast.Active() {
		b.mu.Unlock()
		return 0, ErrBusy
	}
	targets, err := b.adminTargets(ctx)
	if err != nil {
		b.mu.Unlock()
		return 0, err
	}
	b.bcast = BroadcastProgress{Total: len(targets), Started: b.d.Now()}
	b.mu.Unlock()
	for _, chat := range targets {
		out.Bulk(chat, func(ctx context.Context, c *Client) error {
			_, err := c.Send(ctx, chat, text, nil, false)
			return err
		}, func(err error) {
			b.mu.Lock()
			if err == nil {
				b.bcast.Sent++
			} else {
				b.bcast.Failed++
			}
			b.mu.Unlock()
		})
	}
	return len(targets), nil
}

// Progress is the last broadcast as the admin panel shows it.
func (b *AdminBot) Progress() BroadcastProgress {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bcast
}

// --- Mini App: verify + menu URL ---

// MiniAppVerify checks initData against the admin bot and returns the linked admin.
func (b *AdminBot) MiniAppVerify(ctx context.Context, initData string) (db.Admin, error) {
	token, err := b.d.Settings.String(ctx, KeyAdminToken)
	if err != nil || token == "" {
		return db.Admin{}, ErrInitData
	}
	tu, err := CheckInitData(token, initData, b.d.Now())
	if err != nil {
		return db.Admin{}, err
	}
	chat, err := b.getAdminChat(ctx, tu.ID)
	if err != nil {
		return db.Admin{}, ErrInitData
	}
	admin, err := b.d.Store.Q.GetAdmin(ctx, chat.AdminID)
	if err != nil {
		return db.Admin{}, ErrInitData
	}
	if admin.DisabledAt.Valid {
		return db.Admin{}, ErrInitData
	}
	if admin.ExpiresAt.Valid && !time.Unix(admin.ExpiresAt.Int64, 0).After(b.d.Now()) {
		return db.Admin{}, ErrInitData
	}
	return admin, nil
}

// adminBaseURL is https://host:port/<admin path>/ for the Mini App button.
func (b *AdminBot) adminBaseURL(ctx context.Context) string {
	if b.d.AdminBase == nil {
		return ""
	}
	return b.d.AdminBase(ctx)
}

// MiniAppURL is where the admin Mini App opens; "" when Telegram could not load it.
func (b *AdminBot) MiniAppURL(ctx context.Context) string {
	if b.d.MiniApp == nil || !b.d.MiniApp() {
		return ""
	}
	st := b.AdminStatus()
	if !st.Running {
		return ""
	}
	base := b.adminBaseURL(ctx)
	if base == "" {
		return ""
	}
	return base + "/tg"
}

// NotifyChat queues a service message; silently dropped while the bot is off.
func (b *AdminBot) NotifyChat(chat int64, text string) {
	out := b.out.Load()
	if out == nil {
		return
	}
	out.Reply(chat, "", 1, func(ctx context.Context, c *Client) error {
		_, err := c.Send(ctx, chat, text, nil, false)
		return err
	})
}
func fmtBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v, exp = v/unit, exp+1 {
		div *= unit
	}
	units := []string{"KB", "MB", "GB", "TB"}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + units[exp]
}

func esc(s string) string { return html.EscapeString(s) }
