package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cozy/internal/panel/auth"
	"cozy/internal/panel/domain"
	"cozy/internal/panel/store"
	"cozy/internal/panel/store/db"
)

// A 1x1 PNG and a minimal JPEG head for upload tests.
var (
	tinyPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")
	tinyJPG    = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00}, make([]byte, 100)...)
)

func TestAvatarExt(t *testing.T) {
	if avatarExt(tinyPNG) != ".png" || avatarExt(tinyJPG) != ".jpg" || avatarExt([]byte("GIF89a")) != "" {
		t.Fatal("magic detection")
	}
	if _, ok := avatarPath("/data", "../x"); ok {
		t.Fatal("traversal accepted")
	}
	if _, ok := avatarPath("/data", ""); ok {
		t.Fatal("empty accepted")
	}
	if p, ok := avatarPath("/data", "ab12.png"); !ok || p != filepath.Join("/data", "avatars", "ab12.png") {
		t.Fatalf("path: %q %v", p, ok)
	}
}

func TestAvatarUploadServe(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	clock := func() time.Time { return now }
	if err := domain.Seed(ctx, st, now); err != nil {
		t.Fatal(err)
	}
	a, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "admin", PasswordHash: "x", CreatedAt: now.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Q.SetAdminOwner(ctx, db.SetAdminOwnerParams{IsOwner: 1, ID: a.ID}); err != nil {
		t.Fatal(err)
	}
	v, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "viewer", PasswordHash: "x", CreatedAt: now.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Q.UpdateAdminAccess(ctx, db.UpdateAdminAccessParams{DisabledAt: sql.NullInt64{}, ExpiresAt: sql.NullInt64{}, Scopes: `{"users":"r"}`, ID: v.ID}); err != nil {
		t.Fatal(err)
	}
	pool := domain.NewPool(st, clock)
	users := domain.NewUsers(st, pool, noChanges{}, clock)
	tariffs, _ := st.Q.ListTariffs(ctx)
	u, err := users.Create(ctx, domain.CreateInput{Name: "ava", TariffID: tariffs[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewSessions(st.Q, clock, nil)
	ownerToken, ownerSess, err := sessions.Create(ctx, a.ID, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, viewerSess, err := sessions.Create(ctx, v.ID, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	handler, _, err := New(Deps{
		Version: "test", Store: st, Sessions: sessions, Now: clock, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		IPLimit: auth.NewLimiter(10, time.Minute, time.Minute, time.Hour), UserLimit: auth.NewLimiter(10, time.Minute, time.Minute, time.Hour), TOTP: auth.NewTOTPGuard(),
		Users: users, Pool: pool, Changes: noChanges{}, DataDir: dataDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, token, csrf, body string) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, path, nil)
		} else {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		if method != http.MethodGet && method != http.MethodHead {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}
	up := func(token, csrf string, img []byte) *httptest.ResponseRecorder {
		return call(http.MethodPost, "/api/v1/users/"+strconv.FormatInt(u.ID, 10)+"/avatar", token, csrf,
			`{"image":"`+base64.StdEncoding.EncodeToString(img)+`"}`)
	}
	// Garbage is refused, a png passes.
	if rec := up(ownerToken, ownerSess.CsrfToken, []byte("GIF89a....")); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("garbage: %d %s", rec.Code, rec.Body)
	}
	rec := up(ownerToken, ownerSess.CsrfToken, tinyPNG)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	// Read-only eyes see it, blind ones do not.
	get := func(token string) *httptest.ResponseRecorder {
		return call(http.MethodGet, "/api/v1/users/"+strconv.FormatInt(u.ID, 10)+"/avatar", token, "", "")
	}
	if rec := get(viewerToken); rec.Code != http.StatusOK {
		t.Fatalf("viewer serve: %d", rec.Code)
	} else if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type %q", ct)
	} else if !bytes.Equal(rec.Body.Bytes(), tinyPNG) {
		t.Fatal("bytes differ")
	}
	etag := get(viewerToken).Header().Get("ETag")
	if etag == "" {
		t.Fatal("no etag")
	}
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/users/"+strconv.FormatInt(u.ID, 10)+"/avatar", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: viewerToken})
	req.Header.Set("If-None-Match", etag)
	cached := httptest.NewRecorder()
	handler.ServeHTTP(cached, req)
	if cached.Code != http.StatusNotModified {
		t.Fatalf("etag: %d", cached.Code)
	}
	// A reader without grants is refused.
	r, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "blind", PasswordHash: "x", CreatedAt: now.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	blindToken, _, err := sessions.Create(ctx, r.ID, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(blindToken); rec.Code != http.StatusForbidden {
		t.Fatalf("blind serve: %d", rec.Code)
	}
	if rec := up(viewerToken, viewerSess.CsrfToken, tinyJPG); rec.Code != http.StatusForbidden {
		t.Fatalf("reader upload: %d", rec.Code)
	}
	// Replace drops the old file and mints a new name.
	var before string
	if err := st.DB.QueryRowContext(ctx, `SELECT avatar FROM users WHERE id = ?`, u.ID).Scan(&before); err != nil || before == "" {
		t.Fatalf("stored name: %v %q", err, before)
	}
	if rec := up(ownerToken, ownerSess.CsrfToken, tinyJPG); rec.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body)
	}
	var after string
	if err := st.DB.QueryRowContext(ctx, `SELECT avatar FROM users WHERE id = ?`, u.ID).Scan(&after); err != nil || after == before {
		t.Fatalf("name not rotated: %q", after)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "avatars", before)); !os.IsNotExist(err) {
		t.Fatal("old file lingers")
	}
	if rec := get(viewerToken); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("replaced serve: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	// Deleting the user drops the file.
	del, _ := http.NewRequest(http.MethodDelete, "/api/v1/users/"+strconv.FormatInt(u.ID, 10), nil)
	del.AddCookie(&http.Cookie{Name: auth.CookieName, Value: ownerToken})
	del.Header.Set("X-CSRF-Token", ownerSess.CsrfToken)
	delRec := httptest.NewRecorder()
	handler.ServeHTTP(delRec, del)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete user: %d %s", delRec.Code, delRec.Body)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "avatars", after)); !os.IsNotExist(err) {
		t.Fatal("avatar file lingers after user delete")
	}
}
