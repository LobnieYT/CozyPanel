package tgbot

import (
	"context"
	"strings"
	"time"

	"cozy/internal/panel/store/db"
)

// Admin-facing operations shared by the bot callbacks and the panel API.
// Messaging stays with the callers; these mutate and report.

// AdminChatView is one linked chat as the panel shows it.
type AdminChatView struct {
	TgID          int64
	Username      string
	FirstName     string
	Blocked       bool
	CreatedAt     int64
	AdminID       int64
	AdminUsername string
	IsOwner       bool
}

// ListChatsView lists linked chats with their admins.
func (b *AdminBot) ListChatsView(ctx context.Context) ([]AdminChatView, error) {
	rows, err := b.d.Store.DB.QueryContext(ctx, `SELECT c.tg_id, c.username, c.first_name, c.blocked, c.created_at, c.admin_id, a.username, a.is_owner
FROM tg_admin_chats c JOIN admins a ON a.id = c.admin_id ORDER BY c.created_at, c.tg_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminChatView
	for rows.Next() {
		var v AdminChatView
		var blocked, owner int64
		if err := rows.Scan(&v.TgID, &v.Username, &v.FirstName, &blocked, &v.CreatedAt, &v.AdminID, &v.AdminUsername, &owner); err != nil {
			return nil, err
		}
		v.Blocked = blocked != 0
		v.IsOwner = owner != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

// UnlinkChat forgets a chat. The chat's admin keeps working in the panel.
func (b *AdminBot) UnlinkChat(ctx context.Context, tg int64) error {
	return b.dropAdminChat(ctx, tg)
}

// LinkCodeFor mints an owner link code for one admin.
func (b *AdminBot) LinkCodeFor(ctx context.Context, adminID int64) (code string, expires time.Time, err error) {
	secret, err := b.secret(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	now := b.d.Now()
	return AdminLinkCode(secret, adminID, now), now.Add(adminLinkTTL), nil
}

// LinkRequests lists fresh link requests, oldest first.
func (b *AdminBot) LinkRequests() []adminRequestRow {
	return b.pendingRequests(b.d.Now())
}

// ApproveRequest links the requesting chat to its claimed admin. The caller
// messages both sides.
func (b *AdminBot) ApproveRequest(ctx context.Context, tg int64) (db.Admin, error) {
	b.mu.Lock()
	req, found := b.requests[tg]
	if found {
		delete(b.requests, tg)
	}
	b.mu.Unlock()
	if !found || b.d.Now().Sub(req.at) > requestTTL {
		return db.Admin{}, errAdminNotFound
	}
	target, err := b.resolveClaimedAdmin(ctx, req.login)
	if err != nil {
		return db.Admin{}, err
	}
	if err := b.linkChat(ctx, target, tg, &User{Username: req.username, FirstName: req.firstName}, b.d.Now()); err != nil {
		return db.Admin{}, err
	}
	return target, nil
}

// resolveClaimedAdmin maps a claimed login to a linkable (non-owner) admin.
func (b *AdminBot) resolveClaimedAdmin(ctx context.Context, login string) (db.Admin, error) {
	target, err := b.d.Store.Q.GetAdminByUsername(ctx, loginToUsername(login))
	if err != nil {
		return db.Admin{}, errAdminNotFound
	}
	if target.IsOwner != 0 {
		return db.Admin{}, errAdminNotFound
	}
	return target, nil
}

func loginToUsername(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

// RefuseRequest drops a pending request, true when one existed.
func (b *AdminBot) RefuseRequest(tg int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.requests[tg]; !ok {
		return false
	}
	delete(b.requests, tg)
	return true
}
