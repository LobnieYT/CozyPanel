package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cozy/internal/panel/store"
)

// Staged is an uploaded archive, unpacked and verified, waiting for preview
// or apply. Close removes it.
type Staged struct {
	Dir      string
	Manifest Manifest
	DB       *sql.DB
	dbPath   string
}

// OpenArchive streams an upload into a staging dir: gunzip + tar with a size
// cap, tar-slip protection and manifest checks. The database hash is verified.
func OpenArchive(r io.Reader, maxBytes int64) (*Staged, error) {
	dir, err := os.MkdirTemp("", "cozy-restore-")
	if err != nil {
		return nil, err
	}
	st := &Staged{Dir: dir}
	defer func() {
		if err != nil {
			st.Close()
		}
	}()
	if err := unpack(io.LimitReader(r, maxBytes+1), dir, maxBytes); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, ErrFormat
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, ErrFormat
	}
	if m.Format != Format || len(m.Sections) == 0 {
		return nil, ErrFormat
	}
	dbPath := filepath.Join(dir, DatabaseName)
	sum, err := shaFile(dbPath)
	if err != nil {
		return nil, ErrFormat
	}
	if m.DatabaseSHA256 != "" && sum != m.DatabaseSHA256 {
		return nil, fmt.Errorf("database hash mismatch")
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	st.Manifest, st.DB, st.dbPath = m, db, dbPath
	return st, nil
}

// Close drops the staging dir and its database handle.
func (s *Staged) Close() error {
	if s == nil {
		return nil
	}
	if s.DB != nil {
		_ = s.DB.Close()
	}
	return os.RemoveAll(s.Dir)
}

// unpack extracts a gzipped tar under dst: every entry stays inside, regular
// files only, total bytes capped.
func unpack(r io.Reader, dst string, maxBytes int64) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return ErrFormat
	}
	defer gz.Close()
	tw := tar.NewReader(gz)
	var total int64
	seen := map[string]bool{}
	for {
		hdr, err := tw.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return ErrFormat
		}
		if !hdr.FileInfo().Mode().IsRegular() {
			continue
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		if name == "" || name == "." || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			return ErrSlip
		}
		if seen[name] {
			return fmt.Errorf("duplicate entry %q", name)
		}
		seen[name] = true
		total += hdr.Size
		if total > maxBytes || hdr.Size < 0 {
			return ErrTooBig
		}
		target := filepath.Join(dst, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, err = io.CopyN(f, tw, hdr.Size)
		cerr := f.Close()
		if err != nil || cerr != nil {
			return ErrFormat
		}
	}
}

// Preview counts, per section, what the archive holds and how many rows
// already exist locally (by natural key).
type Preview struct {
	Incoming map[string]int `json:"incoming"`
	Overlap  map[string]int `json:"overlap"`
	Panel    string         `json:"panel"`
	Created  int64          `json:"created_at"`
}

// Inspect builds the preview of a staged archive against the live database.
func Inspect(ctx context.Context, st *store.Store, s *Staged) (*Preview, error) {
	p := &Preview{Incoming: map[string]int{}, Overlap: map[string]int{}, Panel: s.Manifest.Panel, Created: s.Manifest.Created}
	for _, sec := range Sections() {
		info, ok := s.Manifest.Sections[sec]
		if !ok {
			continue
		}
		p.Incoming[sec] = info.Count
		n, err := overlap(ctx, st, s, sec)
		if err != nil {
			return nil, err
		}
		p.Overlap[sec] = n
	}
	return p, nil
}

func overlap(ctx context.Context, st *store.Store, s *Staged, sec string) (int, error) {
	switch sec {
	case SectionUsers:
		return overlapKeys(ctx, st.DB, s.DB,
			`SELECT sub_token FROM users`, `SELECT sub_token FROM users`)
	case SectionInbounds:
		return overlapKeys(ctx, st.DB, s.DB,
			`SELECT name FROM inbounds`, `SELECT name FROM inbounds`)
	case SectionNodes:
		return overlapKeys(ctx, st.DB, s.DB,
			`SELECT address FROM nodes WHERE address <> ''`, `SELECT address FROM nodes WHERE address <> ''`)
	case SectionTariffs:
		a, err := overlapKeys(ctx, st.DB, s.DB,
			`SELECT name FROM tariffs`, `SELECT name FROM tariffs`)
		if err != nil {
			return 0, err
		}
		b, err := overlapKeys(ctx, st.DB, s.DB,
			`SELECT name FROM traffic_packages`, `SELECT name FROM traffic_packages`)
		if err != nil {
			return 0, err
		}
		c, err := overlapKeys(ctx, st.DB, s.DB,
			`SELECT name FROM traffic_pools`, `SELECT name FROM traffic_pools`)
		if err != nil {
			return 0, err
		}
		return a + b + c, nil
	case SectionNetwork, SectionSettings:
		return overlapSettings(ctx, st, s, sec == SectionNetwork)
	}
	return 0, fmt.Errorf("unknown section %q", sec)
}

func overlapKeys(ctx context.Context, target, source *sql.DB, targetQ, sourceQ string) (int, error) {
	have := map[string]bool{}
	rows, err := target.QueryContext(ctx, targetQ)
	if err != nil {
		// A table the other side lacks (old backup) is no overlap.
		return 0, nil
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return 0, err
		}
		have[k] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	srows, err := source.QueryContext(ctx, sourceQ)
	if err != nil {
		return 0, nil
	}
	defer srows.Close()
	n := 0
	for srows.Next() {
		var k string
		if err := srows.Scan(&k); err != nil {
			return 0, err
		}
		if have[k] {
			n++
		}
	}
	return n, srows.Err()
}

func overlapSettings(ctx context.Context, st *store.Store, s *Staged, net bool) (int, error) {
	want := map[string]bool{}
	for _, k := range networkKeys {
		if net {
			want[k] = true
		}
	}
	keys := func(db *sql.DB) (map[string]bool, error) {
		out := map[string]bool{}
		rows, err := db.QueryContext(ctx, `SELECT key FROM settings WHERE TRIM(value, ' ') <> ''`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				return nil, err
			}
			if pathKeys[k] {
				continue
			}
			if want[k] == net {
				out[k] = true
			}
		}
		return out, rows.Err()
	}
	have, err := keys(st.DB)
	if err != nil {
		return 0, err
	}
	incoming, err := keys(s.DB)
	if err != nil {
		return 0, nil
	}
	n := 0
	for k := range incoming {
		if have[k] {
			n++
		}
	}
	return n, nil
}
