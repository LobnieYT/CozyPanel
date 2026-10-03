package domain

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"cozy/internal/panel/store"
	"cozy/internal/panel/store/db"
)

// Packages is the catalog of traffic packages (GitHub issue #12): extra traffic for the
// main quota or one pool, given by the admin. Archived packages stay for the grants
// that name them.
type Packages struct {
	st  *store.Store
	now func() time.Time
}

func NewPackages(st *store.Store, now func() time.Time) *Packages { return &Packages{st: st, now: now} }

// PackageInput is a package as the admin sets it.
type PackageInput struct {
	Name     string
	Bytes    int64
	PoolID   int64 // 0: the main traffic
	Lifetime string
	Days     int64 // LifetimeDays
	Sort     int64
}

// A package's name length.
const maxPackageName = 60

func (in *PackageInput) check() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > maxPackageName {
		return fieldErr("name", "name_blank")
	}
	if err := checkGrant(in.Bytes, in.Lifetime, in.Days); err != nil {
		return err
	}
	if in.Lifetime != LifetimeDays {
		in.Days = 0
	}
	return nil
}

func (s *Packages) Create(ctx context.Context, in PackageInput) (db.TrafficPackage, error) {
	if err := in.check(); err != nil {
		return db.TrafficPackage{}, err
	}
	var p db.TrafficPackage
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		if err := poolExists(ctx, q, in.PoolID); err != nil {
			return err
		}
		var err error
		p, err = q.CreateTrafficPackage(ctx, db.CreateTrafficPackageParams{Name: in.Name, Bytes: in.Bytes, PoolID: poolRef(in.PoolID),
			Lifetime: in.Lifetime, Days: in.Days, Sort: in.Sort, CreatedAt: s.now().Unix()})
		return err
	})
	return p, err
}

// Update changes a package. Grants already given keep what they were given.
func (s *Packages) Update(ctx context.Context, id int64, in PackageInput) (db.TrafficPackage, error) {
	if err := in.check(); err != nil {
		return db.TrafficPackage{}, err
	}
	var p db.TrafficPackage
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		if err := poolExists(ctx, q, in.PoolID); err != nil {
			return err
		}
		var err error
		p, err = q.UpdateTrafficPackage(ctx, db.UpdateTrafficPackageParams{Name: in.Name, Bytes: in.Bytes, PoolID: poolRef(in.PoolID),
			Lifetime: in.Lifetime, Days: in.Days, Sort: in.Sort, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return p, err
}

// Archive takes a package off the catalog; given grants keep what they were given.
func (s *Packages) Archive(ctx context.Context, id int64) error {
	n, err := s.st.Q.ArchiveTrafficPackage(ctx, id)
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// PackageFits: the package adds to a quota the user has. Unlimited traffic (the main or a
// pool's) never uses grants, so packages for it are not offered.
func PackageFits(u db.User, pools []db.UserPool, p db.TrafficPackage) bool {
	if !p.PoolID.Valid {
		return u.TrafficLimit.Valid
	}
	for _, up := range pools {
		if up.PoolID == p.PoolID.Int64 {
			return up.TrafficLimit.Valid
		}
	}
	return false
}

func poolRef(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// Flag is a bool as the database keeps it: 1 or 0.
func Flag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
