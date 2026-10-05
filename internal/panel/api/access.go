package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"cozy/internal/panel/store/db"
)

// Grant levels within one section: "r" reads, "w" reads and changes.
const (
	GrantRead  = "r"
	GrantWrite = "w"
)

// Grants maps an API section (operation tag) to a grant level. Absent means
// none; "w" implies "r". The "admins" section is never granted: managing
// admins takes the owner, whatever the matrix says. "auth" stays outside the
// matrix like today (login, own password and own sessions).
type Grants map[string]string

// Sections lists every API tag a grant can cover, in UI order.
func Sections() []string {
	return []string{"users", "inbounds", "node", "network", "settings", "tariffs", "telegram", "stats", "api-keys"}
}

// Presets fills the matrix in one click; the admin then tweaks it by hand.
func Presets() map[string]Grants {
	all := func(level string) Grants {
		g := Grants{}
		for _, s := range Sections() {
			g[s] = level
		}
		return g
	}
	viewer := all(GrantRead)
	operator := all(GrantRead)
	for _, s := range []string{"users", "inbounds", "node"} {
		operator[s] = GrantWrite
	}
	return map[string]Grants{"viewer": viewer, "operator": operator}
}

// ParseGrants reads the stored JSON; unknown sections and levels are refused so
// a typo cannot silently widen access.
func ParseGrants(raw string) (Grants, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Grants{}, nil
	}
	var g Grants
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, s := range Sections() {
		known[s] = true
	}
	for k, v := range g {
		if !known[k] {
			return nil, fmt.Errorf("unknown section %q", k)
		}
		if v != GrantRead && v != GrantWrite {
			return nil, fmt.Errorf("bad grant %q for %q", v, k)
		}
	}
	return g, nil
}

// encodeScopes validates a matrix from the admin UI and stores it.
func encodeScopes(m map[string]string) (string, error) {
	if m == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	if _, err := ParseGrants(string(raw)); err != nil {
		return "", err
	}
	return string(raw), nil
}

// Allows says whether grants cover every tag of an operation; a write
// operation needs "w" everywhere, a read needs at least "r".
func (g Grants) Allows(tags []string, write bool) bool {
	for _, t := range tags {
		got, ok := g[t]
		if !ok {
			return false
		}
		if write && got != GrantWrite {
			return false
		}
	}
	return true
}

// AdminStatus refuses a login or a request of an admin past their term.
func AdminStatus(a db.Admin, now time.Time) error {
	if a.DisabledAt.Valid {
		return huma.Error403Forbidden("account_disabled")
	}
	if a.ExpiresAt.Valid && !time.Unix(a.ExpiresAt.Int64, 0).After(now) {
		return huma.Error403Forbidden("account_expired")
	}
	return nil
}

// authorize loads the session admin and checks the operation against grants.
// The owner passes everything. Expired and disabled accounts stop here, so a
// term ends running sessions too, not just the next login. The "auth" tag is
// identity, not a resource, and stays open like today.
func (h *handlers) authorize(ctx context.Context, sess db.Session, tags []string, mutating bool) (db.Admin, error) {
	admin, err := h.d.Store.Q.GetAdmin(ctx, sess.AdminID)
	if err != nil {
		return db.Admin{}, err
	}
	if err := AdminStatus(admin, h.d.Now()); err != nil {
		return db.Admin{}, err
	}
	if admin.IsOwner != 0 {
		return admin, nil
	}
	rest := make([]string, 0, len(tags))
	for _, t := range tags {
		if t == "admins" {
			return db.Admin{}, huma.Error403Forbidden("no_grant")
		}
		if t != "auth" {
			rest = append(rest, t)
		}
	}
	if len(rest) == 0 {
		return db.Admin{}, huma.Error403Forbidden("no_grant")
	}
	g, err := ParseGrants(admin.Scopes)
	if err != nil {
		h.d.Log.Error("admin scopes", "admin", admin.ID, "err", err)
		return db.Admin{}, huma.Error403Forbidden("no_grant")
	}
	if !g.Allows(rest, mutating) {
		return db.Admin{}, huma.Error403Forbidden("no_grant")
	}
	return admin, nil
}
