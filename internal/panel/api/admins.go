package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"cozy/internal/panel/auth"
	"cozy/internal/panel/store/db"
)

var adminUsernameRe = regexp.MustCompile(`^[a-z0-9._-]{1,64}$`)

func cleanAdminUsername(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	return s, adminUsernameRe.MatchString(s)
}

// requireOwner loads the session admin; only the owner manages admins.
func (h *handlers) requireOwner(ctx context.Context) (db.Admin, error) {
	sess := sessionOf(ctx)
	admin, err := h.d.Store.Q.GetAdmin(ctx, sess.AdminID)
	if err != nil {
		return db.Admin{}, err
	}
	if admin.IsOwner == 0 {
		return db.Admin{}, huma.Error403Forbidden("no_grant")
	}
	return admin, nil
}

type adminIDInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type createAdminInput struct {
	Body struct {
		Username string            `json:"username" minLength:"1" maxLength:"64"`
		Password string            `json:"password" minLength:"12" maxLength:"256" doc:"Не короче 12 символов"`
		Expires  *int64            `json:"expires_at,omitempty" doc:"Конец срока в unix-секундах; null — бессрочно"`
		Scopes   map[string]string `json:"scopes,omitempty" doc:"Раздел → r|w; пусто — только вход"`
	}
}

type updateAdminInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Password *string           `json:"password,omitempty" minLength:"12" maxLength:"256"`
		Disabled *bool             `json:"disabled,omitempty"`
		Expires  *int64            `json:"expires_at,omitempty" doc:"Unix-секунды; 0 — бессрочно"`
		Scopes   map[string]string `json:"scopes,omitempty"`
	}
}

func (h *handlers) registerAdmins() {
	tags := []string{"admins"}
	huma.Register(h.api, huma.Operation{OperationID: "list-admins", Method: http.MethodGet, Path: "/api/v1/admins", Summary: "Администраторы", Tags: tags}, h.listAdmins)
	huma.Register(h.api, huma.Operation{OperationID: "create-admin", Method: http.MethodPost, Path: "/api/v1/admins", Summary: "Создать администратора", Tags: tags, DefaultStatus: http.StatusCreated}, h.createAdmin)
	huma.Register(h.api, huma.Operation{OperationID: "update-admin", Method: http.MethodPatch, Path: "/api/v1/admins/{id}", Summary: "Изменить администратора", Tags: tags}, h.updateAdmin)
	huma.Register(h.api, huma.Operation{OperationID: "delete-admin", Method: http.MethodDelete, Path: "/api/v1/admins/{id}", Summary: "Удалить администратора", Tags: tags, DefaultStatus: http.StatusNoContent}, h.deleteAdmin)
}

type adminsOutput struct {
	Body []AdminView
}

func (h *handlers) listAdmins(ctx context.Context, _ *struct{}) (*adminsOutput, error) {
	if _, err := h.requireOwner(ctx); err != nil {
		return nil, err
	}
	rows, err := h.d.Store.Q.ListAdmins(ctx)
	if err != nil {
		return nil, err
	}
	out := &adminsOutput{Body: []AdminView{}}
	for _, a := range rows {
		out.Body = append(out.Body, viewAdmin(a))
	}
	return out, nil
}

type adminOutput struct {
	Body AdminView
}

func checkExpires(v *int64, now time.Time) (sql.NullInt64, error) {
	if v == nil || *v == 0 {
		return sql.NullInt64{}, nil
	}
	if *v <= now.Unix() {
		return sql.NullInt64{}, huma.Error422UnprocessableEntity("bad_expiry", &huma.ErrorDetail{Location: "body.expires_at", Message: "bad_expiry"})
	}
	return sql.NullInt64{Int64: *v, Valid: true}, nil
}

func (h *handlers) createAdmin(ctx context.Context, in *createAdminInput) (*adminOutput, error) {
	me, err := h.requireOwner(ctx)
	if err != nil {
		return nil, err
	}
	username, ok := cleanAdminUsername(in.Body.Username)
	if !ok {
		return nil, huma.Error422UnprocessableEntity("bad_username", &huma.ErrorDetail{Location: "body.username", Message: "bad_username"})
	}
	if _, err := h.d.Store.Q.GetAdminByUsername(ctx, username); err == nil {
		return nil, huma.Error409Conflict("admin_exists")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	hash, err := auth.HashPassword(in.Body.Password)
	if err != nil {
		return nil, err
	}
	now := h.d.Now()
	raw, err := encodeScopes(in.Body.Scopes)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("bad_scopes", &huma.ErrorDetail{Location: "body.scopes", Message: "bad_scopes"})
	}
	expires, err := checkExpires(in.Body.Expires, now)
	if err != nil {
		return nil, err
	}
	a, err := h.d.Store.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: username, PasswordHash: hash, CreatedAt: now.Unix()})
	if err != nil {
		return nil, err
	}
	if err := h.d.Store.Q.UpdateAdminAccess(ctx, db.UpdateAdminAccessParams{
		DisabledAt: sql.NullInt64{}, ExpiresAt: expires, Scopes: raw, ID: a.ID,
	}); err != nil {
		return nil, err
	}
	a, err = h.d.Store.Q.GetAdmin(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	h.audit(ctx, me.ID, "admin.create", "admin", username, nil)
	return &adminOutput{Body: viewAdmin(a)}, nil
}

func (h *handlers) updateAdmin(ctx context.Context, in *updateAdminInput) (*adminOutput, error) {
	me, err := h.requireOwner(ctx)
	if err != nil {
		return nil, err
	}
	b := in.Body
	id := in.ID
	if id == me.ID {
		return nil, huma.Error403Forbidden("no_grant")
	}
	a, err := h.d.Store.Q.GetAdmin(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, huma.Error404NotFound("not_found")
		}
		return nil, err
	}
	if a.IsOwner != 0 {
		return nil, huma.Error403Forbidden("no_grant")
	}
	now := h.d.Now()
	if b.Password != nil {
		hash, err := auth.HashPassword(*b.Password)
		if err != nil {
			return nil, err
		}
		if err := h.d.Store.Q.SetAdminPassword(ctx, db.SetAdminPasswordParams{PasswordHash: hash, ID: id}); err != nil {
			return nil, err
		}
		if err := h.d.Store.Q.DeleteAdminSessions(ctx, id); err != nil {
			return nil, err
		}
	}
	if b.Disabled != nil || b.Expires != nil || b.Scopes != nil {
		cur, err := h.d.Store.Q.GetAdmin(ctx, id)
		if err != nil {
			return nil, err
		}
		disabled := cur.DisabledAt
		if b.Disabled != nil {
			disabled = sql.NullInt64{}
			if *b.Disabled {
				disabled = sql.NullInt64{Int64: now.Unix(), Valid: true}
			}
		}
		expires := cur.ExpiresAt
		if b.Expires != nil {
			if expires, err = checkExpires(b.Expires, now); err != nil {
				return nil, err
			}
		}
		scopes := cur.Scopes
		if b.Scopes != nil {
			if scopes, err = encodeScopes(b.Scopes); err != nil {
				return nil, huma.Error422UnprocessableEntity("bad_scopes", &huma.ErrorDetail{Location: "body.scopes", Message: "bad_scopes"})
			}
		}
		if err := h.d.Store.Q.UpdateAdminAccess(ctx, db.UpdateAdminAccessParams{DisabledAt: disabled, ExpiresAt: expires, Scopes: scopes, ID: id}); err != nil {
			return nil, err
		}
		if b.Disabled != nil && *b.Disabled {
			if err := h.d.Store.Q.DeleteAdminSessions(ctx, id); err != nil {
				return nil, err
			}
		}
	}
	a, err = h.d.Store.Q.GetAdmin(ctx, id)
	if err != nil {
		return nil, err
	}
	h.audit(ctx, me.ID, "admin.update", "admin", a.Username, nil)
	return &adminOutput{Body: viewAdmin(a)}, nil
}

func (h *handlers) deleteAdmin(ctx context.Context, in *adminIDInput) (*struct{}, error) {
	me, err := h.requireOwner(ctx)
	if err != nil {
		return nil, err
	}
	if in.ID == me.ID {
		return nil, huma.Error403Forbidden("no_grant")
	}
	a, err := h.d.Store.Q.GetAdmin(ctx, in.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, huma.Error404NotFound("not_found")
		}
		return nil, err
	}
	if a.IsOwner != 0 {
		return nil, huma.Error403Forbidden("no_grant")
	}
	// Sessions go with the account (ON DELETE CASCADE); keys keep working until
	// revoked, attributed to a vanished author in the audit trail.
	if err := h.d.Store.Q.DeleteAdmin(ctx, in.ID); err != nil {
		return nil, err
	}
	h.audit(ctx, me.ID, "admin.delete", "admin", a.Username, nil)
	return nil, nil
}
