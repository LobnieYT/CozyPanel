package backup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
