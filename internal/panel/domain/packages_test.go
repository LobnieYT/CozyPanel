package domain

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"cozy/internal/panel/store/db"
)

// The catalog checks its input, and packages fit only quotas the user has.
func TestPackagesCatalog(t *testing.T) {
	e := newGrantsEnv(t, 100)
	pk := NewPackages(e.st, func() time.Time { return *e.now })
	pool, err := e.st.Q.CreateTrafficPool(e.ctx, db.CreateTrafficPoolParams{Name: "WL", CreatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.st.Q.CreateTrafficPool(e.ctx, db.CreateTrafficPoolParams{Name: "Other", CreatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	ok := PackageInput{Name: " +50 GB ", Bytes: 50 * GiB, Lifetime: LifetimeUsed}
	main, err := pk.Create(e.ctx, ok)
	if err != nil || main.Name != "+50 GB" || main.PoolID.Valid {
		t.Fatalf("create: %+v %v", main, err)
	}
	wlIn := ok
	wlIn.PoolID, wlIn.Lifetime, wlIn.Days = pool.ID, LifetimeDays, 30
	wl, err := pk.Create(e.ctx, wlIn)
	if err != nil || wl.Days != 30 || wl.PoolID.Int64 != pool.ID {
		t.Fatalf("pool package: %+v %v", wl, err)
	}
	otherIn := ok
	otherIn.PoolID = other.ID
	otherPkg, err := pk.Create(e.ctx, otherIn)
	if err != nil {
		t.Fatal(err)
	}

	bad := []struct {
		mut   func(*PackageInput)
		field string
	}{
		{func(p *PackageInput) { p.Name = "  " }, "name"},
		{func(p *PackageInput) { p.Bytes = GiB - 1 }, "bytes"},
		{func(p *PackageInput) { p.Bytes = MaxGrantBytes + 1 }, "bytes"},
		{func(p *PackageInput) { p.Lifetime = "x" }, "lifetime"},
		{func(p *PackageInput) { p.Lifetime, p.Days = LifetimeDays, 0 }, "days"},
		{func(p *PackageInput) { p.PoolID = 999 }, "pool_id"},
	}
	for i, c := range bad {
		in := ok
		c.mut(&in)
		var fe *FieldError
		if _, err := pk.Create(e.ctx, in); !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("case %d: %v, want a %s error", i, err, c.field)
		}
	}
	if _, err := pk.Update(e.ctx, 999, ok); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update unknown: %v", err)
	}

	// The user has a main limit and a WL limit; Other is unlimited for them.
	if err := e.st.Q.SetUserPoolLimit(e.ctx, db.SetUserPoolLimitParams{UserID: e.u.ID, PoolID: pool.ID, TrafficLimit: sql.NullInt64{Int64: 10, Valid: true}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.Q.SetUserPoolLimit(e.ctx, db.SetUserPoolLimitParams{UserID: e.u.ID, PoolID: other.ID}); err != nil {
		t.Fatal(err)
	}
	pools, err := e.st.Q.ListUserPools(e.ctx, e.u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !PackageFits(e.u, pools, main) || !PackageFits(e.u, pools, wl) {
		t.Fatal("main and WL packages must fit")
	}
	if PackageFits(e.u, pools, otherPkg) {
		t.Fatal("a package for an unlimited pool must not fit")
	}
	if err := pk.Archive(e.ctx, wl.ID); err != nil {
		t.Fatal(err)
	}
	if err := pk.Archive(e.ctx, wl.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archive twice: %v", err)
	}
}
