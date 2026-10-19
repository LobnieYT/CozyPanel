package app

import (
	"context"
	"log/slog"
	"time"

	"cozy/internal/panel/audit"
	"cozy/internal/panel/store"
	"cozy/internal/panel/store/db"
)

// The owner is the admin who reaches every section and manages the other admins: the
// owner alone is asked about the panel's own parts, and a panel cannot do without one —
// "Администраторы" is closed to everybody else.
//
// The panel is set up by `cozy admin bootstrap`, and its admin is the owner from the
// start (1.3.7). Panels set up before that got an admin without the flag: such an admin
// may sign in and nothing else — every section answered "no grant", and the admins'
// section too, so the panel could not be repaired from within. The owner cannot be
// removed, changed or given away through the panel, so an owner that is missing means
// exactly one thing: the panel was set up the broken way. The first admin (the oldest)
// becomes the owner, and the panel works again.
//
// ensureOwner gives the flag to the first admin when the panel has no owner at all. It
// changes nothing when there is one.
func ensureOwner(ctx context.Context, st *store.Store, log *slog.Logger) error {
	admins, err := st.Q.ListAdmins(ctx)
	if err != nil {
		return err
	}
	if len(admins) == 0 {
		return nil
	}
	for _, a := range admins {
		if a.IsOwner != 0 {
			return nil
		}
	}
	first := admins[0]
	if err := st.Q.SetAdminOwner(ctx, db.SetAdminOwnerParams{IsOwner: 1, ID: first.ID}); err != nil {
		return err
	}
	if log != nil {
		log.Warn("the panel had no owner; the first admin is now the owner", "admin", first.Username)
	}
	_ = audit.Write(ctx, st.Q, time.Now(), audit.Entry{Action: "panel.set_owner", TargetType: "admin", TargetID: first.Username})
	return nil
}
