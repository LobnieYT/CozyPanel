package backup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cozy/internal/panel/store"
)

// files restores the files/ tree (TLS material) over the data dir. Node
// certificate dirs travel with their nodes: nodes/<old> becomes nodes/<new>.
// Overwritten files are set aside first, so a failure below rolls back.
func (a *applier) applyFiles(dataDir, stagingDir string) error {
	if dataDir == "" {
		return nil
	}
	src := filepath.Join(stagingDir, "files", "tls")
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	aside := filepath.Join(stagingDir, "files-backup")
	type written struct{ target, backup string }
	var done []written
	fail := func(err error) error {
		for i := len(done) - 1; i >= 0; i-- {
			w := done[i]
			if w.backup == "" {
				_ = os.Remove(w.target)
				continue
			}
			_ = os.Rename(w.backup, w.target)
		}
		return err
	}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("link refused: %s", path)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 2 && parts[0] == "nodes" {
			old, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return fmt.Errorf("bad node dir %q", rel)
			}
			id, ok := a.nodes[old]
			if !ok {
				a.warnf("certificates of missing node skipped")
				return nil
			}
			parts[1] = strconv.FormatInt(id, 10)
			rel = filepath.Join(parts...)
		}
		target := filepath.Join(dataDir, "tls", rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fail(err)
		}
		w := written{target: target}
		if raw, err := os.ReadFile(target); err == nil {
			keep := filepath.Join(aside, filepath.ToSlash(rel))
			if err := os.MkdirAll(filepath.Dir(keep), 0o700); err != nil {
				return fail(err)
			}
			if err := os.WriteFile(keep, raw, 0o600); err != nil {
				return fail(err)
			}
			w.backup = keep
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fail(err)
		}
		if err := os.WriteFile(target, raw, 0o600); err != nil {
			return fail(err)
		}
		done = append(done, w)
		return nil
	})
	if err != nil {
		return fail(err)
	}
	return nil
}

// applyAvatars restores the avatars over the data dir when users are imported.
// Names are random per upload, so staged files never clobber live ones with
// different content; files neither staged nor referenced by the imported users
// are dropped, so a restore does not pile up orphans. Symlinks are refused
// like everywhere else in the archive.
func applyAvatars(st *store.Store, dataDir, stagingDir string) error {
	if dataDir == "" {
		return nil
	}
	src := filepath.Join(stagingDir, "files", "avatars")
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	dst := filepath.Join(dataDir, "avatars")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	staged := map[string]bool{}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("link refused: %s", path)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel != filepath.Base(rel) {
			return fmt.Errorf("nested file refused: %s", rel)
		}
		staged[rel] = true
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0o600)
	})
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	rows, err := st.DB.Query(`SELECT avatar FROM users WHERE avatar IS NOT NULL AND avatar != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		keep[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || staged[e.Name()] || keep[e.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dst, e.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
