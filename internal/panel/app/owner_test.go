package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"cozy/internal/panel/auth"
	"cozy/internal/panel/store"
	"cozy/internal/panel/store/db"
)

// A panel set up before 1.3.7 has an admin without the owner flag: he may sign in and
// nothing else — every section answered "no grant", the admins' section included, so the
// panel could not be repaired from within. The first admin becomes the owner at the start
// and the panel works.
func TestOwnerlessAdminReachesTheSections(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	// The harness makes its admin an owner; take the flag away, as a panel set up before
	// 1.3.7 had it.
	admins, err := h.st.Q.ListAdmins(ctx)
	if err != nil || len(admins) != 1 {
		t.Fatalf("admins: %v %d", err, len(admins))
	}
	if err := h.st.Q.SetAdminOwner(ctx, db.SetAdminOwnerParams{IsOwner: 0, ID: admins[0].ID}); err != nil {
		t.Fatal(err)
	}
	h.login(password, "")
	resp, _ := h.do(http.MethodGet, "/"+adminPath+"/api/v1/settings", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("without the owner the section is closed: %d", resp.StatusCode)
	}
	if err := ensureOwner(ctx, h.st, nil); err != nil {
		t.Fatal(err)
	}
	h.login(password, "")
	resp, _ = h.do(http.MethodGet, "/"+adminPath+"/api/v1/settings", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the owner reaches the section: %d", resp.StatusCode)
	}
}

// A panel that has an owner keeps it: the repair never touches an owner that is there.
func TestPanelWithAnOwnerKeepsIt(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	before, err := h.st.Q.ListAdmins(ctx)
	if err != nil || len(before) != 1 || before[0].IsOwner == 0 {
		t.Fatalf("the harness admin is not the owner: %v %+v", err, before)
	}
	if err := ensureOwner(ctx, h.st, nil); err != nil {
		t.Fatal(err)
	}
	after, err := h.st.Q.ListAdmins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].IsOwner == 0 {
		t.Fatalf("the owner is gone: %+v", after)
	}
}

// A panel with no admins at all is left alone: there is nobody to make the owner.
func TestPanelWithoutAdminsIsLeftAlone(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	if err := h.st.Q.DeleteAdmin(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwner(ctx, h.st, nil); err != nil {
		t.Fatal(err)
	}
	admins, err := h.st.Q.ListAdmins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(admins) != 0 {
		t.Fatalf("admins appeared out of nowhere: %+v", admins)
	}
}

// The panel itself makes its owner: a panel set up the broken way (an admin without the
// flag) gets one as it starts, and the owner reaches the sections that are his alone.
func TestNewPanelMakesTheOwner(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "admin", PasswordHash: hash, CreatedAt: time.Now().Unix()})
	if err != nil {
		t.Fatal(err)
	}
	// No owner flag: the panel of 1.3.6 and older.
	web := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><html><head><!-- cozy:base --></head><body></body></html>")},
		"sub.html":   {Data: []byte("<!doctype html><html><head><!-- cozy:base --></head><body>sub</body></html>")},
	}
	if _, err := NewPanel(st, Options{Version: "test", Web: web, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now, Resolve: testResolve}); err != nil {
		t.Fatal(err)
	}
	admins, err := st.Q.ListAdmins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(admins) != 1 || admins[0].IsOwner == 0 {
		t.Fatalf("the panel did not make its owner: %+v", admins)
	}
	if admins[0].ID != a.ID {
		t.Fatalf("another admin became the owner: %+v", admins)
	}
}
