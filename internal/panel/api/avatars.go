package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"cozy/internal/panel/auth"
)

// Avatars ride with users: one picture per subscription, uploaded in the
// panel. Files live under data/avatars under random names (a re-upload gets a
// new name, so browsers cache hard and the old file is dropped); the users
// row keeps the name, "" for none. Serving needs the users grant like the
// card itself; <img> carries the session cookie, so no token dance is needed.

const avatarMaxBytes = 2 << 20 // decoded image bytes

func avatarDir(dataDir string) string { return filepath.Join(dataDir, "avatars") }

// avatarOf reads the stored file name, "" for none or a missing user.
func (h *handlers) avatarOf(ctx context.Context, userID int64) (string, error) {
	var name sql.NullString
	if err := h.d.Store.DB.QueryRowContext(ctx, `SELECT avatar FROM users WHERE id = ?`, userID).Scan(&name); err != nil {
		return "", err
	}
	if !name.Valid {
		return "", nil
	}
	return name.String, nil
}

func (h *handlers) setAvatar(ctx context.Context, userID int64, name string) error {
	_, err := h.d.Store.DB.ExecContext(ctx, `UPDATE users SET avatar = ?, updated_at = ? WHERE id = ?`, name, h.d.Now().Unix(), userID)
	return err
}

// avatarPath resolves a stored name inside the avatars dir; hostile names from
// older or hand-made rows never escape it.
func avatarPath(dataDir, name string) (string, bool) {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return "", false
	}
	return filepath.Join(avatarDir(dataDir), name), true
}

func (h *handlers) removeAvatarFile(name string) {
	if h.d.DataDir == "" || name == "" {
		return
	}
	if path, ok := avatarPath(h.d.DataDir, name); ok {
		_ = os.Remove(path)
	}
}

func avatarExt(raw []byte) string {
	if len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF {
		return ".jpg"
	}
	if len(raw) >= 8 && raw[0] == 0x89 && raw[1] == 0x50 && raw[2] == 0x4E && raw[3] == 0x47 &&
		raw[4] == 0x0D && raw[5] == 0x0A && raw[6] == 0x1A && raw[7] == 0x0A {
		return ".png"
	}
	return ""
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type avatarUploadInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Image string `json:"image" minLength:"50" maxLength:"4000000" doc:"Картинка base64: jpeg или png до 2 МБ"`
	}
}

type avatarUploadOutput struct {
	Body struct {
		Avatar bool `json:"avatar"`
	}
}

func (h *handlers) registerAvatars() {
	tags := []string{"users"}
	huma.Register(h.api, huma.Operation{OperationID: "upload-avatar", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPost, Path: "/api/v1/users/{id}/avatar", Summary: "Поставить аватарку", Tags: tags}, h.uploadAvatar)
}

func (h *handlers) uploadAvatar(ctx context.Context, in *avatarUploadInput) (*avatarUploadOutput, error) {
	if _, err := h.d.Users.Get(ctx, in.ID); err != nil {
		return nil, mapDomainErr(err)
	}
	raw, err := base64.StdEncoding.DecodeString(in.Body.Image)
	if err != nil || len(raw) == 0 || len(raw) > avatarMaxBytes {
		return nil, huma.Error422UnprocessableEntity("bad_image")
	}
	ext := avatarExt(raw)
	if ext == "" {
		return nil, huma.Error422UnprocessableEntity("bad_image")
	}
	if h.d.DataDir == "" {
		return nil, huma.Error409Conflict("no_data_dir")
	}
	name, err := randHex(16)
	if err != nil {
		return nil, err
	}
	name += ext
	if err := os.MkdirAll(avatarDir(h.d.DataDir), 0o700); err != nil {
		return nil, err
	}
	old, err := h.avatarOf(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(avatarDir(h.d.DataDir), name), raw, 0o600); err != nil {
		return nil, err
	}
	if err := h.setAvatar(ctx, in.ID, name); err != nil {
		_ = os.Remove(filepath.Join(avatarDir(h.d.DataDir), name))
		return nil, err
	}
	h.removeAvatarFile(old)
	h.audit(ctx, sessionOf(ctx).AdminID, "user.avatar", "user", strconv.FormatInt(in.ID, 10), nil)
	return &avatarUploadOutput{Body: struct {
		Avatar bool `json:"avatar"`
	}{Avatar: true}}, nil
}

// registerAvatarFile mounts the picture itself: <img> cannot send the
// openapi-fetch headers, but the session cookie travels with same-origin
// requests, and the grants check below matches the card's.
func (h *handlers) registerAvatarFile(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users/{id}/avatar", h.serveAvatar)
}

func (h *handlers) serveAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 || h.d.DataDir == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	ck, err := r.Cookie(auth.CookieName)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	sess, err := h.d.Sessions.Lookup(r.Context(), ck.Value)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if _, err := h.authorize(r.Context(), sess, []string{"users"}, false); err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	name, err := h.avatarOf(r.Context(), id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	path, ok := avatarPath(h.d.DataDir, name)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	etag := `"` + name + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=86400, must-revalidate")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	ct := "image/jpeg"
	if strings.HasSuffix(name, ".png") {
		ct = "image/png"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	_, _ = w.Write(raw)
}
