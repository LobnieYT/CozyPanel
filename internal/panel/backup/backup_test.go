package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"cozy/internal/panel/store"
)

func seed(t *testing.T, ctx context.Context, st *store.Store) {
	t.Helper()
	now := time.Now().Unix()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO tariffs (name, duration_days, created_at) VALUES ('base', 30, ?)`, now)
	exec(`INSERT INTO slots (name, uuid, secret, state, created_at) VALUES ('s000001', 'u1', 'sec1', 'assigned', ?)`, now)
	exec(`INSERT INTO users (name, sub_token, period_start, created_at, updated_at, slot_id, tariff_id) VALUES ('u1', 'tok1', ?, ?, ?, 1, 1)`, now, now, now)
	exec(`INSERT INTO inbounds (node_id, name, preset, port, enabled, settings, created_at, updated_at) VALUES (1, 'in1', 'hysteria2', '443', 1, '{}', ?, ?)`, now, now)
	exec(`UPDATE nodes SET address = '203.0.113.7' WHERE id = 1`)
	exec(`INSERT INTO settings (key, value) VALUES ('node_dns', '{"enable":false}')`)
}

func openStore(t *testing.T, ctx context.Context) *store.Store {
	t.Helper()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func makeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOpenArchiveRefuses(t *testing.T) {
	if _, err := OpenArchive(strings.NewReader("not a gzip"), MaxArchive); err == nil {
		t.Fatal("plain text accepted")
	}
	evil := makeArchive(t, map[string]string{
		ManifestName:  `{"format":1,"panel":"x","created_at":1,"sections":{}}`,
		"../evil.txt": "x",
		DatabaseName:  "x",
	})
	if _, err := OpenArchive(bytes.NewReader(evil), MaxArchive); err == nil {
		t.Fatal("tar-slip accepted")
	}
	badManifest := makeArchive(t, map[string]string{ManifestName: `{"format":999}`, DatabaseName: "x"})
	if _, err := OpenArchive(bytes.NewReader(badManifest), MaxArchive); err == nil {
		t.Fatal("unknown format accepted")
	}
	noDB := makeArchive(t, map[string]string{ManifestName: `{"format":1,"panel":"x","created_at":1,"sections":{"users":{"count":0}}}`})
	if _, err := OpenArchive(bytes.NewReader(noDB), MaxArchive); err == nil {
		t.Fatal("missing database accepted")
	}
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := openStore(t, ctx)
	seed(t, ctx, src)

	path, name, err := Export(ctx, src, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if name == "" {
		t.Fatal("no download name")
	}
	dst := openStore(t, ctx)
	staged, err := func() (*Staged, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return OpenArchive(f, MaxArchive)
	}()
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if staged.Manifest.Format != Format {
		t.Fatalf("manifest: %+v", staged.Manifest)
	}
	pv, err := Inspect(ctx, dst, staged)
	if err != nil {
		t.Fatal(err)
	}
	if pv.Incoming[SectionUsers] != 1 || pv.Overlap[SectionUsers] != 0 {
		t.Fatalf("preview: %+v", pv)
	}
	out, err := Apply(ctx, dst, "", staged, ApplyRequest{Sections: Sections(), Strategy: StrategyReplace}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if out.Sections[SectionUsers].Inserted != 1 || out.Sections[SectionInbounds].Inserted != 1 {
		t.Fatalf("applied: %+v", out)
	}
	// Idempotent replace, then skip-everything.
	out2, err := Apply(ctx, dst, "", staged, ApplyRequest{Sections: Sections(), Strategy: StrategyReplace}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if out2.Sections[SectionUsers].Updated != 1 {
		t.Fatalf("re-replace: %+v", out2)
	}
	out3, err := Apply(ctx, dst, "", staged, ApplyRequest{Sections: Sections(), Strategy: StrategySkip}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if out3.Sections[SectionUsers].Skipped != 1 {
		t.Fatalf("skip: %+v", out3)
	}
	// Fresh mints new credentials.
	out4, err := Apply(ctx, dst, "", staged, ApplyRequest{Sections: []string{SectionUsers}, Strategy: StrategyFresh}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if out4.Sections[SectionUsers].Inserted != 1 {
		t.Fatalf("fresh: %+v", out4)
	}
	var n int
	if err := dst.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("users: %d %v", n, err)
	}
	// The handed-out link survived replace; fresh minted another token.
	var kept, distinct int
	if err := dst.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE sub_token = 'tok1'`).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("token: %d %v", kept, err)
	}
	if err := dst.DB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT sub_token) FROM users`).Scan(&distinct); err != nil || distinct != 2 {
		t.Fatalf("distinct: %d %v", distinct, err)
	}
}
