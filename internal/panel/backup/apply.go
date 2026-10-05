package backup

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"cozy/internal/panel/store"
)

// ApplyRequest selects sections and the conflict strategy.
type ApplyRequest struct {
	Sections []string
	Strategy Strategy
}

// SectionResult counts what a section did.
type SectionResult struct {
	Inserted int `json:"inserted"`
	Updated  int `json:"updated"`
	Skipped  int `json:"skipped"`
}

// Applied is the outcome, with notes on what was left out and why.
type Applied struct {
	Sections map[string]SectionResult `json:"sections"`
	Warnings []string                 `json:"warnings,omitempty"`
}

// Apply imports the selected sections of a staged archive into the live
// database, in dependency order, in one transaction: any failure rolls
// everything back. Files land after the commit, from aside copies on failure.
func Apply(ctx context.Context, st *store.Store, dataDir string, s *Staged, req ApplyRequest, version string) (*Applied, error) {
	if err := checkStrategy(req.Strategy); err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, sec := range req.Sections {
		known := false
		for _, k := range Sections() {
			if sec == k {
				known = true
			}
		}
		if !known {
			return nil, fmt.Errorf("%w: %q", ErrNeedSection, sec)
		}
		want[sec] = true
	}
	if len(want) == 0 {
		return nil, ErrNoSections
	}
	out := &Applied{Sections: map[string]SectionResult{}}
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	rolledBack := false
	defer func() {
		if !rolledBack {
			_ = tx.Rollback()
		}
	}()
	a := &applier{tx: tx, src: s.DB, strategy: req.Strategy, out: out}
	if want[SectionTariffs] {
		if err := a.applyTariffs(ctx); err != nil {
			return nil, err
		}
	}
	if want[SectionNodes] {
		if err := a.applyNodes(ctx); err != nil {
			return nil, err
		}
	}
	if want[SectionInbounds] {
		if err := a.applyInbounds(ctx); err != nil {
			return nil, err
		}
	}
	if want[SectionUsers] {
		if err := a.applyUsers(ctx); err != nil {
			return nil, err
		}
	}
	if want[SectionNetwork] {
		if err := a.applySettings(ctx, true); err != nil {
			return nil, err
		}
	}
	if want[SectionSettings] {
		if err := a.applySettings(ctx, false); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	rolledBack = true
	if want[SectionNodes] {
		if err := a.applyFiles(dataDir, s.Dir); err != nil {
			return nil, err
		}
	}
	_ = version
	return out, nil
}

// applier carries the id remaps between the archive and the live database.
type applier struct {
	tx       *sql.Tx
	src      *sql.DB
	strategy Strategy
	out      *Applied
	warn     []string

	tariffs  map[int64]int64
	pools    map[int64]int64
	packages map[int64]int64
	nodes    map[int64]int64
	inbounds map[int64]int64
}

func (a *applier) warnf(format string, args ...any) {
	a.out.Warnings = append(a.out.Warnings, fmt.Sprintf(format, args...))
}

func (a *applier) count(sec string, kind string) {
	r := a.out.Sections[sec]
	switch kind {
	case "insert":
		r.Inserted++
	case "update":
		r.Updated++
	default:
		r.Skipped++
	}
	a.out.Sections[sec] = r
}

// rows reads a whole table from either database as column maps.
func rows(ctx context.Context, db *sql.DB, query string, args ...any) ([]map[string]any, error) {
	r, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	cols, err := r.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for r.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := r.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out, r.Err()
}

func placeholders(n int) string {
	q := make([]string, n)
	for i := range q {
		q[i] = "?"
	}
	return strings.Join(q, ", ")
}

// insertRow writes a row map, skipping the id column (fresh ids, remapped by
// callers that keep them).
func insertRow(ctx context.Context, tx *sql.Tx, table string, row map[string]any, keep []string) (int64, error) {
	cols := []string{}
	vals := []any{}
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sortStrings(keys)
	keepSet := map[string]bool{}
	for _, k := range keep {
		keepSet[k] = true
	}
	for _, k := range keys {
		if k == "id" && !keepSet["id"] {
			continue
		}
		cols = append(cols, k)
		vals = append(vals, row[k])
	}
	res, err := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table,
		strings.Join(cols, ", "), placeholders(len(cols))), vals...)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// updateRow overwrites a row by id, except the id itself.
func updateRow(ctx context.Context, tx *sql.Tx, table string, id int64, row map[string]any, skip ...string) error {
	skipSet := map[string]bool{"id": true}
	for _, k := range skip {
		skipSet[k] = true
	}
	keys := []string{}
	for k := range row {
		if !skipSet[k] {
			keys = append(keys, k)
		}
	}
	sortStrings(keys)
	set := make([]string, 0, len(keys))
	vals := make([]any, 0, len(keys)+1)
	for _, k := range keys {
		set = append(set, k+" = ?")
		vals = append(vals, row[k])
	}
	vals = append(vals, id)
	_, err := tx.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", table, strings.Join(set, ", ")), vals...)
	return err
}

func (a *applier) byName(ctx context.Context, tx *sql.Tx, table string) (map[string]int64, error) {
	// Reads the TARGET side (tx) for remapping.
	out := map[string]int64{}
	// NOTE: read-your-writes inside tx: rows() takes *sql.DB; wrap tx.
	rows, err := txRows(ctx, tx, fmt.Sprintf("SELECT id, name FROM %s", table))
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if name, ok := r["name"].(string); ok {
			if _, dup := out[name]; !dup {
				out[name] = toInt(r["id"])
			}
		}
	}
	return out, nil
}

func txRows(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]map[string]any, error) {
	r, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	cols, err := r.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for r.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := r.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out, r.Err()
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
