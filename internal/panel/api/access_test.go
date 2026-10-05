package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"cozy/internal/panel/auth"
	"cozy/internal/panel/domain"
	"cozy/internal/panel/store"
	"cozy/internal/panel/store/db"
)

func TestGrants(t *testing.T) {
	if _, err := ParseGrants(`{"users":"w","bogus":"r"}`); err == nil {
		t.Fatal("unknown section accepted")
	}
	if _, err := ParseGrants(`{"users":"x"}`); err == nil {
		t.Fatal("unknown level accepted")
	}
	g, err := ParseGrants(`{"users":"w","node":"r"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Allows([]string{"users"}, true) || !g.Allows([]string{"node"}, false) {
		t.Fatal("grants do not allow what they name")
	}
	if g.Allows([]string{"node"}, true) || g.Allows([]string{"settings"}, false) {
		t.Fatal("grants allow what they do not name")
	}
	for name, p := range Presets() {
		for _, s := range Sections() {
			if _, ok := p[s]; !ok {
				t.Fatalf("preset %s misses %s", name, s)
			}
		}
	}
	if _, ok := Presets()["viewer"]["users"]; !ok {
		t.Fatal("viewer preset misses users")
	}
}

func TestAdminStatus(t *testing.T) {
	now := time.Now()
	if err := AdminStatus(db.Admin{}, now); err != nil {
		t.Fatalf("fresh admin refused: %v", err)
	}
	if err := AdminStatus(db.Admin{DisabledAt: sql.NullInt64{Int64: now.Unix(), Valid: true}}, now); err == nil {
		t.Fatal("disabled admin passes")
	}
	if err := AdminStatus(db.Admin{ExpiresAt: sql.NullInt64{Int64: now.Add(-time.Hour).Unix(), Valid: true}}, now); err == nil {
		t.Fatal("expired admin passes")
	}
	if err := AdminStatus(db.Admin{ExpiresAt: sql.NullInt64{Int64: now.Add(time.Hour).Unix(), Valid: true}}, now); err != nil {
		t.Fatalf("future term refused: %v", err)
	}
}

func TestAccessMatrixHTTP(t *testing.T) {
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
	mkAdmin := func(name string, owner bool, scopes map[string]string, disabled, expired bool) int64 {
		t.Helper()
		a, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: name, PasswordHash: "x", CreatedAt: now.Unix()})
		if err != nil {
			t.Fatal(err)
		}
		if owner {
			if err := st.Q.SetAdminOwner(ctx, db.SetAdminOwnerParams{IsOwner: 1, ID: a.ID}); err != nil {
				t.Fatal(err)
			}
		}
		raw, _ := json.Marshal(scopes)
		dis := sql.NullInt64{}
		if disabled {
			dis = sql.NullInt64{Int64: now.Unix(), Valid: true}
		}
		exp := sql.NullInt64{}
		if expired {
			exp = sql.NullInt64{Int64: now.Add(-time.Hour).Unix(), Valid: true}
		}
		if err := st.Q.UpdateAdminAccess(ctx, db.UpdateAdminAccessParams{DisabledAt: dis, ExpiresAt: exp, Scopes: string(raw), ID: a.ID}); err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	ownerID := mkAdmin("owner", true, nil, false, false)
	viewerID := mkAdmin("viewer", false, map[string]string{"users": "r"}, false, false)
	opID := mkAdmin("operator", false, map[string]string{"users": "w"}, false, false)
	expID := mkAdmin("expired", false, map[string]string{"users": "w"}, false, true)
	disID := mkAdmin("disabled", false, map[string]string{"users": "w"}, true, false)

	sessions := auth.NewSessions(st.Q, clock, nil)
	type cred struct {
		cookie *http.Cookie
		csrf   string
	}
	credOf := func(id int64) cred {
		t.Helper()
		token, sess, err := sessions.Create(ctx, id, "127.0.0.1", "test")
		if err != nil {
			t.Fatal(err)
		}
		return cred{cookie: &http.Cookie{Name: auth.CookieName, Value: token}, csrf: sess.CsrfToken}
	}
	creds := map[int64]cred{
		ownerID:  credOf(ownerID),
		viewerID: credOf(viewerID),
		opID:     credOf(opID),
		expID:    credOf(expID),
		disID:    credOf(disID),
	}

	pool := domain.NewPool(st, clock)
	users := domain.NewUsers(st, pool, noChanges{}, clock)
	handler, openapi, err := New(Deps{
		Version: "test", Store: st, Sessions: sessions, Now: clock, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		IPLimit: auth.NewLimiter(10, time.Minute, time.Minute, time.Hour), UserLimit: auth.NewLimiter(10, time.Minute, time.Minute, time.Hour), TOTP: auth.NewTOTPGuard(),
		Users: users, Pool: pool, Changes: noChanges{},
	})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, c cred, body string) int {
		t.Helper()
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, path, nil)
		} else {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		}
		r.AddCookie(c.cookie)
		if method != http.MethodGet && method != http.MethodHead {
			r.Header.Set("X-CSRF-Token", c.csrf)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec.Code
	}
	// Viewer reads users but changes nothing, and never sees admins.
	if code := call(http.MethodGet, "/api/v1/users?state=all", creds[viewerID], ""); code != http.StatusOK {
		t.Fatalf("viewer list users: %d", code)
	}
	if code := call(http.MethodPost, "/api/v1/users", creds[viewerID], "{}"); code != http.StatusForbidden {
		t.Fatalf("viewer create user: %d, want 403", code)
	}
	if code := call(http.MethodGet, "/api/v1/admins", creds[viewerID], ""); code != http.StatusForbidden {
		t.Fatalf("viewer list admins: %d, want 403", code)
	}
	// Operator writes users (422 proves it passed auth) but not admins.
	if code := call(http.MethodPost, "/api/v1/users", creds[opID], "{}"); code == http.StatusForbidden {
		t.Fatal("operator create user refused")
	}
	if code := call(http.MethodGet, "/api/v1/admins", creds[opID], ""); code != http.StatusForbidden {
		t.Fatalf("operator list admins: %d, want 403", code)
	}
	// Past terms and disabled accounts stop everywhere.
	for id, name := range map[int64]string{expID: "expired", disID: "disabled"} {
		if code := call(http.MethodGet, "/api/v1/users?state=all", creds[id], ""); code != http.StatusForbidden {
			t.Fatalf("%s list users: %d, want 403", name, code)
		}
	}
	// Owner passes and manages admins.
	if code := call(http.MethodGet, "/api/v1/admins", creds[ownerID], ""); code != http.StatusOK {
		t.Fatalf("owner list admins: %d", code)
	}
	// Every operation tag is either identity (auth), owner-only (admins) or a
	// section of the matrix: an unknown tag must never silently pass or fail.
	known := map[string]bool{"auth": true, "admins": true}
	for _, s := range Sections() {
		known[s] = true
	}
	for path, item := range openapi.OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Put, item.Post, item.Delete, item.Patch, item.Options, item.Head} {
			if op == nil {
				continue
			}
			for _, tag := range op.Tags {
				if !known[tag] {
					t.Errorf("%s: unknown tag %q", path, tag)
				}
			}
		}
	}
}
