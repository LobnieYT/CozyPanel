package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cozy/internal/panel/store"
)

// networkKeys are the settings that travel as the "network" section.
var networkKeys = []string{"node_dns", "node_routes", "node_outbounds", "sub_dns", "node_adblock", "sub_adblock"}

// pathKeys identify this panel's own addresses: exported (full server) but
// never imported, or the panel would move out from under the admin.
var pathKeys = map[string]bool{"admin_path": true, "sub_path": true, "sub_port": true}

// Export writes a full-server archive to a temp file and returns its path and
// download name. The caller removes the file after serving it.
func Export(ctx context.Context, st *store.Store, dataDir, version string) (path, name string, err error) {
	work, err := os.MkdirTemp("", "cozy-export-")
	if err != nil {
		return "", "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(work)
		}
	}()
	dbPath := filepath.Join(work, DatabaseName)
	f, err := os.OpenFile(dbPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", "", err
	}
	if err := f.Close(); err != nil {
		return "", "", err
	}
	if _, err := st.DB.ExecContext(ctx, "VACUUM INTO ?", dbPath); err != nil {
		return "", "", fmt.Errorf("copy database: %w", err)
	}
	filesDir := filepath.Join(work, "files")
	manifest, err := describe(ctx, st, dataDir, version, filesDir)
	if err != nil {
		return "", "", err
	}
	if manifest.DatabaseSHA256, err = shaFile(dbPath); err != nil {
		return "", "", err
	}
	rawManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", "", err
	}
	out, err := os.CreateTemp("", "cozy-backup-*.tar.gz")
	if err != nil {
		return "", "", err
	}
	outPath := out.Name()
	when := time.Unix(manifest.Created, 0).UTC()
	if err := pack(out, when, map[string][]byte{ManifestName: rawManifest}, dbPath, filesDir); err != nil {
		_ = out.Close()
		_ = os.Remove(outPath)
		return "", "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(outPath)
		return "", "", err
	}
	name = fmt.Sprintf("cozy-backup-%s.tar.gz", when.Format("20060102-150405"))
	return outPath, name, nil
}

// describe counts the sections and stages the files/ tree.
func describe(ctx context.Context, st *store.Store, dataDir, version, filesDir string) (*Manifest, error) {
	m := &Manifest{Format: Format, Panel: version, Created: time.Now().UTC().Unix(), Sections: map[string]SectionInfo{}}
	count := func(query string) (int, error) {
		var n int
		if err := st.DB.QueryRowContext(ctx, query).Scan(&n); err != nil {
			return 0, err
		}
		return n, nil
	}
	counts := map[string]int{}
	var err error
	if counts[SectionUsers], err = count(`SELECT COUNT(*) FROM users`); err != nil {
		return nil, err
	}
	if counts[SectionInbounds], err = count(`SELECT COUNT(*) FROM inbounds`); err != nil {
		return nil, err
	}
	if counts[SectionNodes], err = count(`SELECT COUNT(*) FROM nodes`); err != nil {
		return nil, err
	}
	var tariffs, packages, pools int
	if tariffs, err = count(`SELECT COUNT(*) FROM tariffs`); err != nil {
		return nil, err
	}
	if packages, err = count(`SELECT COUNT(*) FROM traffic_packages`); err != nil {
		return nil, err
	}
	if pools, err = count(`SELECT COUNT(*) FROM traffic_pools`); err != nil {
		return nil, err
	}
	counts[SectionTariffs] = tariffs + packages + pools
	net, set, err := countSettings(ctx, st)
	if err != nil {
		return nil, err
	}
	counts[SectionNetwork] = net
	counts[SectionSettings] = set
	for _, s := range Sections() {
		m.Sections[s] = SectionInfo{Count: counts[s]}
	}
	if dataDir != "" {
		if err := stageFiles(filepath.Join(dataDir, "tls"), filepath.Join(filesDir, "tls")); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return m, nil
}

func countSettings(ctx context.Context, st *store.Store) (net, set int, err error) {
	rows, err := st.DB.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	isNet := map[string]bool{}
	for _, k := range networkKeys {
		isNet[k] = true
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return 0, 0, err
		}
		if strings.TrimSpace(v) == "" {
			continue
		}
		if isNet[k] {
			net++
		} else {
			set++
		}
	}
	return net, set, rows.Err()
}

// stageFiles copies a tree of small secrets (TLS material) into the archive
// staging dir. Symlinks are refused: the archive must not reach outside data.
func stageFiles(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("link refused: %s", path)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600)
	})
}

// pack writes manifest blobs, the database copy and the files tree into a
// gzipped tar stream. Timestamps come from the manifest, so a rebuild of the
// same content is byte-identical.
func pack(w io.Writer, when time.Time, blobs map[string][]byte, dbPath, filesDir string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	add := func(name string, data []byte, mode int64) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), ModTime: when}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	for name, data := range blobs {
		if err := add(name, data, 0o600); err != nil {
			return err
		}
	}
	db, err := os.ReadFile(dbPath)
	if err != nil {
		return err
	}
	if err := add(DatabaseName, db, 0o600); err != nil {
		return err
	}
	if st, err := os.Stat(filesDir); err == nil && st.IsDir() {
		err = filepath.WalkDir(filesDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(filesDir, path)
			if err != nil {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return add("files/"+filepath.ToSlash(rel), raw, 0o600)
		})
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func shaFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
