package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"cozy/internal/panel/auth"
	"cozy/internal/panel/server"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/store/db"
	"cozy/internal/panel/tgbot"
)

// The admin bot in the panel: a second bot with its own token. Configuration
// is owner-only (tag "admins", like the admins page itself): whoever sets the
// token decides where it talks. The bot's own commands enforce the matrix.

type adminBotChatView struct {
	TgID          int64  `json:"tg_id"`
	Username      string `json:"username"`
	FirstName     string `json:"first_name"`
	AdminID       int64  `json:"admin_id"`
	AdminUsername string `json:"admin_username"`
	IsOwner       bool   `json:"is_owner"`
	CreatedAt     string `json:"created_at"`
}

type adminBotView struct {
	Enabled    bool               `json:"enabled"`
	TokenSet   bool               `json:"token_set"`
	TokenHint  string             `json:"token_hint,omitempty"`
	Running    bool               `json:"running"`
	Error      string             `json:"error,omitempty"`
	Bot        *TelegramBot       `json:"bot,omitempty"`
	Linked     []adminBotChatView `json:"linked"`
	Route      TelegramRoute      `json:"route"`
	Broadcast  *TelegramBroadcast `json:"broadcast,omitempty"`
	MiniAppURL string             `json:"mini_app_url,omitempty"`
}

type adminBotOutput struct{ Body adminBotView }

type patchAdminBotInput struct {
	Body struct {
		Enabled *bool   `json:"enabled,omitempty"`
		Token   *string `json:"token,omitempty" maxLength:"100" doc:"Токен от @BotFather; пустая строка — удалить"`
		Route   *struct {
			Mode   string  `json:"mode" enum:"direct,node,proxy"`
			NodeID int64   `json:"node_id,omitempty" minimum:"0"`
			Proxy  *string `json:"proxy,omitempty" maxLength:"512"`
		} `json:"route,omitempty"`
	}
}

type adminLinkCodeInput struct {
	Body struct {
		AdminID *int64 `json:"admin_id,omitempty" doc:"Чей код; пусто — свой"`
	}
}

type adminLinkCodeOutput struct {
	Body struct {
		Code      string `json:"code"`
		ExpiresAt string `json:"expires_at"`
	}
}

type adminRequestsOutput struct {
	Body struct {
		Requests []adminRequestView `json:"requests"`
	}
}

type adminRequestView struct {
	TgID        int64  `json:"tg_id"`
	Username    string `json:"username"`
	FirstName   string `json:"first_name"`
	Login       string `json:"login"`
	RequestedAt string `json:"requested_at"`
}

type adminChatInput struct {
	TgID int64 `path:"tg" minimum:"1"`
}

type adminBroadcastInput struct {
	Body struct {
		Text string `json:"text" minLength:"1" maxLength:"3500"`
	}
}

type adminBroadcastOutput struct {
	Body struct {
		Queued int `json:"queued"`
	}
}

func (h *handlers) registerAdminBot() {
	tags := []string{"admins"}
	huma.Register(h.api, huma.Operation{OperationID: "get-admin-bot", Method: http.MethodGet, Path: "/api/v1/admin-bot", Summary: "Админ-бот", Tags: tags}, h.getAdminBot)
	huma.Register(h.api, huma.Operation{OperationID: "update-admin-bot", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPatch, Path: "/api/v1/admin-bot", Summary: "Настроить админ-бота", Tags: tags}, h.updateAdminBot)
	huma.Register(h.api, huma.Operation{OperationID: "admin-bot-link-code", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPost, Path: "/api/v1/admin-bot/link-code", Summary: "Код привязки чата", Tags: tags}, h.adminBotLinkCode)
	huma.Register(h.api, huma.Operation{OperationID: "admin-bot-requests", Method: http.MethodGet, Path: "/api/v1/admin-bot/requests", Summary: "Заявки на привязку", Tags: tags}, h.adminBotRequests)
	huma.Register(h.api, huma.Operation{OperationID: "admin-bot-approve", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPost, Path: "/api/v1/admin-bot/requests/{tg}/approve", Summary: "Подтвердить заявку", Tags: tags}, h.adminBotApprove)
	huma.Register(h.api, huma.Operation{OperationID: "admin-bot-refuse", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPost, Path: "/api/v1/admin-bot/requests/{tg}/refuse", Summary: "Отклонить заявку", Tags: tags}, h.adminBotRefuse)
	huma.Register(h.api, huma.Operation{OperationID: "admin-bot-unlink", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodDelete, Path: "/api/v1/admin-bot/chats/{tg}", Summary: "Отвязать чат", Tags: tags, DefaultStatus: http.StatusNoContent}, h.adminBotUnlink)
	huma.Register(h.api, huma.Operation{OperationID: "admin-bot-broadcast", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPost, Path: "/api/v1/admin-bot/broadcast", Summary: "Разослать привязанным админам", Tags: tags, DefaultStatus: http.StatusAccepted}, h.adminBotBroadcast)
}

func (h *handlers) adminBotView(ctx context.Context) (adminBotView, error) {
	var v adminBotView
	v.Linked = []adminBotChatView{}
	var err error
	if v.Enabled, err = h.d.Settings.On(ctx, tgbot.AdminEnabled); err != nil {
		return v, err
	}
	token, err := h.d.Settings.String(ctx, tgbot.KeyAdminToken)
	if err != nil {
		return v, err
	}
	if token != "" {
		v.TokenSet = true
		if id, _, ok := cutToken(token); ok {
			v.TokenHint = id
		}
	}
	if bot, ok, _ := settings.Get[tgbot.User](ctx, h.d.Settings, tgbot.KeyAdminBot); ok && token != "" {
		v.Bot = &TelegramBot{Username: bot.Username, Name: bot.FirstName}
	}
	if h.d.AdminBot != nil {
		st := h.d.AdminBot.AdminStatus()
		v.Running, v.Error = st.Running, st.Error
		if chats, err := h.d.AdminBot.ListChatsView(ctx); err != nil {
			return v, err
		} else {
			for _, c := range chats {
				v.Linked = append(v.Linked, adminBotChatView{TgID: c.TgID, Username: c.Username, FirstName: c.FirstName,
					AdminID: c.AdminID, AdminUsername: c.AdminUsername, IsOwner: c.IsOwner,
					CreatedAt: time.Unix(c.CreatedAt, 0).UTC().Format(time.RFC3339)})
			}
		}
		if p := h.d.AdminBot.Progress(); p.Total > 0 {
			v.Broadcast = &TelegramBroadcast{Total: p.Total, Sent: p.Sent, Failed: p.Failed, Started: p.Started.Unix(), Active: p.Active()}
		}
		v.MiniAppURL = h.d.AdminBot.MiniAppURL(ctx)
	}
	route := tgbot.Route{Mode: tgbot.RouteDirect}
	if h.d.AdminBot != nil {
		if route, err = h.d.AdminBot.LoadAdminRoute(ctx); err != nil {
			return v, err
		}
	}
	v.Route = TelegramRoute{Mode: route.Mode, NodeID: route.NodeID, Proxy: tgbot.MaskProxy(route.Proxy)}
	return v, nil
}

func (h *handlers) getAdminBot(ctx context.Context, _ *struct{}) (*adminBotOutput, error) {
	v, err := h.adminBotView(ctx)
	if err != nil {
		return nil, err
	}
	return &adminBotOutput{Body: v}, nil
}

func (h *handlers) updateAdminBot(ctx context.Context, in *patchAdminBotInput) (*adminBotOutput, error) {
	if h.d.AdminBot == nil {
		return nil, huma.Error503ServiceUnavailable("bot_unavailable")
	}
	b := in.Body
	details := map[string]any{}
	route, err := h.d.AdminBot.LoadAdminRoute(ctx)
	if err != nil {
		return nil, err
	}
	if b.Route != nil {
		if route, err = h.nextRoute(ctx, route, b.Route.Mode, b.Route.NodeID, b.Route.Proxy, details); err != nil {
			return nil, err
		}
	}
	token, err := h.d.Settings.String(ctx, tgbot.KeyAdminToken)
	if err != nil {
		return nil, err
	}
	var bot *tgbot.User
	if b.Token != nil {
		token = *b.Token
		if token == "" {
			details["token"] = "removed"
		} else {
			if _, _, ok := cutToken(token); !ok {
				return nil, tgFieldErr("token", "tg_token_format")
			}
			me, err := h.d.AdminBot.CheckAdminTokenVia(ctx, token, route)
			var ae *tgbot.APIError
			switch {
			case errors.As(err, &ae) && (ae.Code == 401 || ae.Code == 404):
				return nil, tgFieldErr("token", "tg_token_invalid")
			case err != nil && b.Route != nil && route.Mode != tgbot.RouteDirect:
				return nil, tgFieldErr("route", "tg_route_unreachable")
			case err != nil:
				return nil, huma.Error502BadGateway("tg_unreachable")
			}
			bot = &me
			details["token"], details["bot"] = "set", me.Username
		}
	}
	if b.Route != nil && route.Mode != tgbot.RouteDirect && bot == nil {
		if err := h.d.AdminBot.CheckAdminRoute(ctx, route, token); err != nil {
			return nil, tgFieldErr("route", "tg_route_unreachable")
		}
	}
	if b.Enabled != nil && *b.Enabled && token == "" {
		return nil, tgFieldErr("enabled", "tg_no_token")
	}
	err = h.d.Store.Tx(ctx, func(q *db.Queries) error {
		set := settings.New(q)
		if b.Route != nil {
			if err := settings.Set(ctx, set, tgbot.KeyAdminRoute, route); err != nil {
				return err
			}
		}
		if b.Token != nil {
			if err := settings.Set(ctx, set, tgbot.KeyAdminToken, token); err != nil {
				return err
			}
			if bot != nil {
				if err := settings.Set(ctx, set, tgbot.KeyAdminBot, *bot); err != nil {
					return err
				}
			} else if err := settings.Set(ctx, set, tgbot.KeyAdminEnabled, false); err != nil {
				return err
			}
		}
		if b.Enabled != nil {
			return settings.Set(ctx, set, tgbot.KeyAdminEnabled, *b.Enabled)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if b.Enabled != nil {
		details["enabled"] = *b.Enabled
	}
	h.d.AdminBot.Reload()
	h.audit(ctx, sessionOf(ctx).AdminID, "adminbot.update", "admin-bot", "", details)
	v, err := h.adminBotView(ctx)
	if err != nil {
		return nil, err
	}
	return &adminBotOutput{Body: v}, nil
}

func (h *handlers) adminBotLinkCode(ctx context.Context, in *adminLinkCodeInput) (*adminLinkCodeOutput, error) {
	if h.d.AdminBot == nil {
		return nil, huma.Error503ServiceUnavailable("bot_unavailable")
	}
	id := sessionOf(ctx).AdminID
	if in.Body.AdminID != nil {
		id = *in.Body.AdminID
	}
	if _, err := h.d.Store.Q.GetAdmin(ctx, id); err != nil {
		return nil, huma.Error404NotFound("not_found")
	}
	code, expires, err := h.d.AdminBot.LinkCodeFor(ctx, id)
	if err != nil {
		return nil, err
	}
	h.audit(ctx, sessionOf(ctx).AdminID, "adminbot.link_code", "admin", strconv.FormatInt(id, 10), nil)
	return &adminLinkCodeOutput{Body: struct {
		Code      string `json:"code"`
		ExpiresAt string `json:"expires_at"`
	}{Code: code, ExpiresAt: expires.UTC().Format(time.RFC3339)}}, nil
}

func (h *handlers) adminBotRequests(ctx context.Context, _ *struct{}) (*adminRequestsOutput, error) {
	out := &adminRequestsOutput{}
	out.Body.Requests = []adminRequestView{}
	if h.d.AdminBot == nil {
		return out, nil
	}
	for _, r := range h.d.AdminBot.LinkRequests() {
		out.Body.Requests = append(out.Body.Requests, adminRequestView{TgID: r.TgID, Username: r.Username,
			FirstName: r.FirstName, Login: r.Login, RequestedAt: r.At.UTC().Format(time.RFC3339)})
	}
	return out, nil
}

func (h *handlers) adminBotApprove(ctx context.Context, in *adminChatInput) (*struct{}, error) {
	if h.d.AdminBot == nil {
		return nil, huma.Error503ServiceUnavailable("bot_unavailable")
	}
	target, err := h.d.AdminBot.ApproveRequest(ctx, in.TgID)
	if err != nil {
		return nil, huma.Error404NotFound("not_found")
	}
	h.d.AdminBot.NotifyChat(in.TgID, "Доступ открыт. /help — команды.")
	h.audit(ctx, sessionOf(ctx).AdminID, "adminbot.approve", "admin", target.Username, map[string]any{"tg_id": in.TgID})
	return &struct{}{}, nil
}

func (h *handlers) adminBotRefuse(ctx context.Context, in *adminChatInput) (*struct{}, error) {
	if h.d.AdminBot == nil {
		return nil, huma.Error503ServiceUnavailable("bot_unavailable")
	}
	if !h.d.AdminBot.RefuseRequest(in.TgID) {
		return nil, huma.Error404NotFound("not_found")
	}
	h.d.AdminBot.NotifyChat(in.TgID, "Владелец отклонил заявку.")
	h.audit(ctx, sessionOf(ctx).AdminID, "adminbot.refuse", "admin", "", map[string]any{"tg_id": in.TgID})
	return &struct{}{}, nil
}

func (h *handlers) adminBotUnlink(ctx context.Context, in *adminChatInput) (*struct{}, error) {
	if h.d.AdminBot == nil {
		return nil, huma.Error503ServiceUnavailable("bot_unavailable")
	}
	if err := h.d.AdminBot.UnlinkChat(ctx, in.TgID); err != nil {
		return nil, err
	}
	h.audit(ctx, sessionOf(ctx).AdminID, "adminbot.unlink", "admin", "", map[string]any{"tg_id": in.TgID})
	return nil, nil
}

func (h *handlers) adminBotBroadcast(ctx context.Context, in *adminBroadcastInput) (*adminBroadcastOutput, error) {
	if h.d.AdminBot == nil {
		return nil, huma.Error503ServiceUnavailable("bot_unavailable")
	}
	n, err := h.d.AdminBot.Broadcast(ctx, in.Body.Text)
	if err != nil {
		return nil, huma.Error409Conflict("bot_off")
	}
	h.audit(ctx, sessionOf(ctx).AdminID, "adminbot.broadcast", "admin-bot", "", map[string]any{"queued": n})
	return &adminBroadcastOutput{Body: struct {
		Queued int `json:"queued"`
	}{Queued: n}}, nil
}

// registerAdminTG mounts the Mini App page and its session bridge under the
// admin path: GET /tg serves the admin bundle framed for Telegram, POST
// /tg/session trades init_data for a session cookie.
func (h *handlers) registerAdminTG(mux *http.ServeMux, spa http.Handler) {
	if spa == nil {
		return
	}
	mux.HandleFunc("GET /tg", func(w http.ResponseWriter, r *http.Request) {
		spa.ServeHTTP(wrapTgFrame{w, r}, r)
	})
	mux.HandleFunc("POST /tg/session", h.adminTGSession)
}

type wrapTgFrame struct {
	http.ResponseWriter
	r *http.Request
}

func (w wrapTgFrame) WriteHeader(code int) {
	hd := w.Header()
	hd.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; frame-ancestors https://web.telegram.org; base-uri 'self'; form-action 'self'; object-src 'none'")
	hd.Del("X-Frame-Options")
	w.ResponseWriter.WriteHeader(code)
}

func (h *handlers) adminTGSession(w http.ResponseWriter, r *http.Request) {
	fail := func() {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"init_data"}`))
	}
	if h.d.AdminBot == nil {
		fail()
		return
	}
	if server.FetchSite(r.Header, r.Host) == server.SiteCross {
		fail()
		return
	}
	var in struct {
		InitData string `json:"init_data"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&in) != nil || in.InitData == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	admin, err := h.d.AdminBot.MiniAppVerify(r.Context(), in.InitData)
	if err != nil {
		fail()
		return
	}
	c := r.Context()
	ip := server.ClientIP(r.Header, r.RemoteAddr, h.d.TrustProxy)
	token, sess, err := h.d.Sessions.Create(c, admin.ID, ip, r.UserAgent())
	if err != nil {
		fail()
		return
	}
	now := h.d.Now()
	if err := h.d.Store.Q.SetAdminLastLogin(c, db.SetAdminLastLoginParams{LastLoginAt: sql.NullInt64{Int64: now.Unix(), Valid: true}, ID: admin.ID}); err != nil {
		h.d.Log.Warn("adminbot: last login not saved", "err", err)
	}
	h.audit(c, admin.ID, "adminbot.session", "admin", admin.Username, nil)
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(auth.MaxTTL / time.Second)})
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"csrf_token": sess.CsrfToken})
}
