package tgbot

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"cozy/internal/panel/store/db"
)

func TestAdminLinkCode(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1_800_000_000, 0)
	code := AdminLinkCode(secret, 7, now)
	if len(code) > 64 || strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
		t.Fatalf("a /start parameter is at most 64 of [A-Za-z0-9_-]: %q", code)
	}
	if id, err := ParseAdminLinkCode(secret, code, now.Add(adminLinkTTL-time.Second)); err != nil || id != 7 {
		t.Fatalf("fresh code: %d %v", id, err)
	}
	if _, err := ParseAdminLinkCode(secret, code, now.Add(adminLinkTTL+time.Second)); !errors.Is(err, ErrLinkExpired) {
		t.Fatalf("stale code: %v", err)
	}
	if _, err := ParseAdminLinkCode([]byte("another secret, another panel..."), code, now); !errors.Is(err, ErrLinkInvalid) {
		t.Fatalf("another panel's code: %v", err)
	}
	// Domain separation: a subscriber code never opens the admin bot and back.
	sub := LinkCode(secret, 7, now)
	if _, err := ParseAdminLinkCode(secret, sub, now); !errors.Is(err, ErrLinkInvalid) {
		t.Fatalf("subscriber code parsed as admin: %v", err)
	}
	if _, err := ParseLinkCode(secret, code, now); !errors.Is(err, ErrLinkInvalid) {
		t.Fatalf("admin code parsed as subscriber: %v", err)
	}
}

func TestAdminCan(t *testing.T) {
	owner := db.Admin{ID: 1, Username: "root", IsOwner: 1, Scopes: "{}"}
	if err := adminCan(owner, "users", true); err != nil {
		t.Fatalf("owner passes everything: %v", err)
	}
	op := db.Admin{ID: 2, Username: "op", Scopes: `{"users":"w","stats":"r","bogus_section":"w","node":"x"}`}
	if err := adminCan(op, "users", true); err != nil {
		t.Fatalf("write grant: %v", err)
	}
	if err := adminCan(op, "stats", false); err != nil {
		t.Fatalf("read grant: %v", err)
	}
	if err := adminCan(op, "stats", true); !errors.Is(err, errAdminNoGrant) {
		t.Fatalf("read-only used for write: %v", err)
	}
	if err := adminCan(op, "settings", false); !errors.Is(err, errAdminNoGrant) {
		t.Fatalf("missing section: %v", err)
	}
	if err := adminCan(op, "admins", false); !errors.Is(err, errAdminNoGrant) {
		t.Fatalf("admins section: %v", err)
	}
	if err := adminCan(op, "node", false); !errors.Is(err, errAdminNoGrant) {
		t.Fatalf("bad level: %v", err)
	}
	// Unknown sections in stored scopes are ignored, not refused.
	if err := adminCan(op, "users", false); err != nil {
		t.Fatalf("unknown section nearby: %v", err)
	}
	broken := db.Admin{ID: 3, Username: "broken", Scopes: `{"users":`}
	if err := adminCan(broken, "users", false); !errors.Is(err, errAdminNoGrant) {
		t.Fatalf("broken scopes: %v", err)
	}
	off := db.Admin{ID: 4, Username: "off", Scopes: `{"users":"w"}`, DisabledAt: sql.NullInt64{Int64: 1, Valid: true}}
	if err := adminCan(off, "users", true); !errors.Is(err, errAdminClosed) {
		t.Fatalf("disabled: %v", err)
	}
	old := db.Admin{ID: 5, Username: "old", Scopes: `{"users":"w"}`, ExpiresAt: sql.NullInt64{Int64: 1, Valid: true}}
	if err := adminCan(old, "users", true); !errors.Is(err, errAdminClosed) {
		t.Fatalf("expired: %v", err)
	}
}
