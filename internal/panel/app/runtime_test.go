package app

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cozy/internal/panel/domain"
	"cozy/internal/panel/server"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/subs"
)

// Run returns only when every worker has: the database is closed after it, and a worker
// still at work would be using a closed one.
func TestRunAllWaitsForEveryWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var finished atomic.Int32
	slow := func(ctx context.Context) {
		<-ctx.Done()
		time.Sleep(150 * time.Millisecond) // finishing what it was doing
		finished.Add(1)
	}
	returned := make(chan struct{})
	go func() {
		runAll(ctx, slow, slow, func(ctx context.Context) { <-ctx.Done(); finished.Add(1) })
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("runAll returned before its context ended")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	<-returned
	if finished.Load() != 3 {
		t.Fatalf("runAll returned with %d of 3 workers finished", finished.Load())
	}
}

func TestConfigCache(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	var gen atomic.Uint64
	c := &configCache{now: func() time.Time { return now }, gen: gen.Load}
	var builds int
	build := func(context.Context) (subs.Config, error) {
		builds++
		return subs.Config{Brand: "b" + string(rune('0'+builds))}, nil
	}
	get := func() string {
		cfg, err := c.get(context.Background(), build)
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Brand
	}
	if get() != "b1" || get() != "b1" || builds != 1 {
		t.Fatalf("a config is built once for requests close together: %d builds", builds)
	}
	gen.Add(1) // a setting, a node or a certificate changed in this process
	if get() != "b2" {
		t.Fatal("a change is seen at once")
	}
	now = now.Add(configCacheTTL + time.Second) // one made by the CLI is seen in the end
	if get() != "b3" {
		t.Fatal("an old config is built again")
	}
	now = now.Add(-time.Hour) // the clock moved back: nothing is trusted
	if get() != "b4" {
		t.Fatal("a config from the future is not served")
	}
	// Errors are not kept.
	gen.Add(1)
	fail := true
	flaky := func(context.Context) (subs.Config, error) {
		if fail {
			return subs.Config{}, errors.New("database is locked")
		}
		return subs.Config{Brand: "ok"}, nil
	}
	if _, err := c.get(context.Background(), flaky); err == nil {
		t.Fatal("the error is lost")
	}
	fail = false
	if cfg, err := c.get(context.Background(), flaky); err != nil || cfg.Brand != "ok" {
		t.Fatalf("after an error: %+v %v", cfg, err)
	}
}

// A setting changed through the API is in the next subscription, though the config is kept.
func TestSubscriptionSeesSettingsAtOnce(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := domain.Seed(ctx, h.st, h.now); err != nil {
		t.Fatal(err)
	}
	set := settings.New(h.st.Q)
	for k, v := range map[string]any{settings.KeyPublicHost: "203.0.113.10", settings.KeyPanelPort: 21355, settings.KeyBrand: "First"} {
		if err := settings.Set(ctx, set, k, v); err != nil {
			t.Fatal(err)
		}
	}
	clock := func() time.Time { return h.now }
	tariffs, _ := h.st.Q.ListTariffs(ctx)
	u, err := domain.NewUsers(h.st, domain.NewPool(h.st, clock), noChanges{}, clock).Create(ctx, domain.CreateInput{Name: "a", TariffID: tariffs[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	title := func() string {
		t.Helper()
		resp, _ := h.do(http.MethodGet, "/"+subPath+"/"+u.SubToken, nil, map[string]string{"User-Agent": "Happ/3.4.1"})
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(resp.Header.Get("Profile-Title"), "base64:"))
		if err != nil {
			t.Fatalf("title %q: %v", resp.Header.Get("Profile-Title"), err)
		}
		return string(raw)
	}
	if title() != "First" || title() != "First" {
		t.Fatal("brand")
	}
	if err := settings.Set(ctx, set, settings.KeyBrand, "Second"); err != nil {
		t.Fatal(err)
	}
	if got := title(); got != "Second" {
		t.Fatalf("a changed brand came as %q", got)
	}
}

// With nothing the app can use, it gets a placeholder that says so, not a profile with an
// empty group that the app refuses whole.
func TestSubscriptionWithoutServersIsAStub(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := domain.Seed(ctx, h.st, h.now); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]any{settings.KeyPublicHost: "203.0.113.10", settings.KeyPanelPort: 21355} {
		if err := settings.Set(ctx, settings.New(h.st.Q), k, v); err != nil {
			t.Fatal(err)
		}
	}
	clock := func() time.Time { return h.now }
	tariffs, _ := h.st.Q.ListTariffs(ctx)
	u, err := domain.NewUsers(h.st, domain.NewPool(h.st, clock), noChanges{}, clock).Create(ctx, domain.CreateInput{Name: "a", TariffID: tariffs[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	fetch := func(ua string) (int, string) {
		resp, body := h.do(http.MethodGet, "/"+subPath+"/"+u.SubToken, nil, map[string]string{"User-Agent": ua})
		return resp.StatusCode, string(body)
	}
	if code, body := fetch("mihomo/1.19.31"); code != http.StatusOK || !strings.Contains(body, `"proxies": [`) || strings.Contains(body, "подходящих серверов") {
		t.Fatalf("with servers: %d %.200s", code, body)
	}
	if _, err := h.st.DB.ExecContext(ctx, "UPDATE inbounds SET enabled = 0"); err != nil {
		t.Fatal(err)
	}
	code, body := fetch("mihomo/1.19.31")
	if code != http.StatusOK || !strings.Contains(body, "подходящих серверов") || !strings.Contains(body, "socks5") {
		t.Fatalf("clash without servers: %d %.300s", code, body)
	}
	code, body = fetch("Happ/3.4.1")
	raw, err := base64.StdEncoding.DecodeString(body)
	if code != http.StatusOK || err != nil || !strings.Contains(string(raw), "vless://") || !strings.Contains(string(raw), "%D0%BD%D0%B5%D1%82") {
		t.Fatalf("links without servers: %d %q %v", code, raw, err)
	}
}

// Every answer of a panel that serves a trusted certificate asks browsers to keep to HTTPS;
// one that does not (development, a proxy in front, a self-signed fallback) does not claim it.
func TestHSTSOnlyWhereTheTLSIsOurs(t *testing.T) {
	for _, on := range []bool{false, true} {
		h := newHarness(t, func(o *Options) { o.HSTS = func() bool { return on } })
		for _, path := range []string{"/" + adminPath + "/", "/" + subPath + "/", "/nothing-here"} {
			resp, _ := h.do(http.MethodGet, path, nil, nil)
			got := resp.Header.Get("Strict-Transport-Security")
			if on && got != server.HSTSValue || !on && got != "" {
				t.Fatalf("hsts=%v %s: header %q", on, path, got)
			}
		}
	}
	if strings.Contains(server.HSTSValue, "includeSubDomains") || strings.Contains(server.HSTSValue, "preload") {
		t.Fatal("the panel does not know what else its domain serves")
	}
}
