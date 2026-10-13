package nodealert

import (
	"context"
	"strings"
	"testing"
	"time"

	"cozy/internal/nodeapi"
	"cozy/internal/panel/nodesync"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
)

type watcherEnv struct {
	w    *Watcher
	now  time.Time
	hv   nodesync.HealthView
	sent []string
}

func setupWatcher(t *testing.T) *watcherEnv {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set := settings.New(st.Q)
	now := time.Unix(1_800_000_000, 0)
	e := &watcherEnv{now: now}
	clock := func() time.Time { return e.now }
	for k, v := range map[string]any{KeyEnabled: true, KeyCPU: 90, KeyMem: 90, KeyMinutes: 3, KeyCooldown: 1} {
		if err := settings.Set(ctx, set, k, v); err != nil {
			t.Fatal(err)
		}
	}
	e.hv = nodesync.HealthView{OK: true, CheckedAt: now, Health: nodeapi.Health{
		System: nodeapi.System{CPUPercent: 10, MemTotal: 8 << 30, MemUsed: 1 << 30},
	}}
	e.w = New(Deps{Store: st, Settings: set, Now: clock,
		Log: discardLog{},
		Health: func(id int64) (nodesync.HealthView, bool) {
			hv := e.hv
			hv.CheckedAt = e.now
			return hv, true
		},
		Nodes: func(context.Context) ([]Node, error) {
			return []Node{{ID: 1, Name: "n1", Enabled: true}}, nil
		},
		Notify: func(_ context.Context, text string) { e.sent = append(e.sent, text) },
		Tick:   time.Minute,
	})
	return e
}

type discardLog struct{}

func (discardLog) Warn(string, ...any)  {}
func (discardLog) Error(string, ...any) {}

func (e *watcherEnv) tick(cpu float64, memUsed uint64) {
	e.hv.Health.System.CPUPercent = cpu
	e.hv.Health.System.MemUsed = memUsed
	e.w.once(context.Background())
	e.now = e.now.Add(time.Minute)
}

func texts(e *watcherEnv) string { return strings.Join(e.sent, "\n") }

func TestWatcherHoldFireRecover(t *testing.T) {
	e := setupWatcher(t)
	e.tick(95, 1<<30)
	e.tick(95, 1<<30)
	if len(e.sent) != 0 {
		t.Fatalf("fired before hold: %q", texts(e))
	}
	e.tick(95, 1<<30)
	if len(e.sent) != 1 || !strings.Contains(texts(e), "CPU") {
		t.Fatalf("no fire on hold: %q", texts(e))
	}
	// Still breaching, already firing: quiet.
	e.tick(95, 1<<30)
	e.tick(95, 1<<30)
	if len(e.sent) != 1 {
		t.Fatalf("duplicate fire: %q", texts(e))
	}
	// Dip without hysteresis: still firing, quiet.
	e.tick(85, 1<<30)
	if len(e.sent) != 1 {
		t.Fatalf("dip refired: %q", texts(e))
	}
	// Recovery below hysteresis: notice + rearm.
	e.tick(10, 1<<30)
	if len(e.sent) != 2 || !strings.Contains(texts(e), "норма") {
		t.Fatalf("no recovery: %q", texts(e))
	}
	// Cooldown (1h) blocks the immediate next incident...
	e.tick(95, 1<<30)
	e.tick(95, 1<<30)
	e.tick(95, 1<<30)
	if len(e.sent) != 2 {
		t.Fatalf("cooldown ignored: %q", texts(e))
	}
	// ...but a new day fires again.
	e.now = e.now.Add(25 * time.Hour)
	e.tick(95, 1<<30)
	e.tick(95, 1<<30)
	e.tick(95, 1<<30)
	if len(e.sent) != 3 {
		t.Fatalf("no refire next day: %q", texts(e))
	}
}

func TestWatcherMem(t *testing.T) {
	e := setupWatcher(t)
	e.tick(10, 8<<30-100)
	e.tick(10, 8<<30-100)
	e.tick(10, 8<<30-100)
	if len(e.sent) != 1 || !strings.Contains(texts(e), "RAM") {
		t.Fatalf("mem fire: %q", texts(e))
	}
}

func TestWatcherOff(t *testing.T) {
	e := setupWatcher(t)
	ctx := context.Background()
	if err := settings.Set(ctx, e.w.d.Settings, KeyEnabled, false); err != nil {
		t.Fatal(err)
	}
	e.tick(99, 8<<30)
	e.tick(99, 8<<30)
	e.tick(99, 8<<30)
	if len(e.sent) != 0 {
		t.Fatalf("fires while off: %q", texts(e))
	}
}
