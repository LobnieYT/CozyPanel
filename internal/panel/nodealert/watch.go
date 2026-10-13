package nodealert

import (
	"context"
	"fmt"
	"html"
	"sync"
	"time"

	"cozy/internal/panel/nodesync"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
)

// Resource alerts: the bot tells linked admins when a node runs out of CPU or
// RAM. The panel host is covered by its local node, whose system counters are
// host-wide. Short spikes must not page anyone: a breach fires only after it
// holds, and re-arms only after a recovery below threshold minus hysteresis.

// Settings keys.
const (
	KeyEnabled  = "alert_enabled"
	KeyCPU      = "alert_cpu_percent"
	KeyMem      = "alert_mem_percent"
	KeyMinutes  = "alert_minutes"
	KeyCooldown = "alert_cooldown_hours"
)

// Enabled switches the alerts on.
var Enabled = settings.Switch{Key: KeyEnabled}

// Defaults, as approved: 90% CPU/RAM held 5 minutes, at most every 6 hours.
const (
	DefCPU      = 90
	DefMem      = 90
	DefMinutes  = 5
	DefCooldown = 6
	Hysteresis  = 10 // re-arm points below the threshold
)

type Deps struct {
	Store    *store.Store
	Settings *settings.Settings
	Now      func() time.Time
	Log      interface {
		Warn(msg string, args ...any)
		Error(msg string, args ...any)
	}
	// Health is the nodes' live view.
	Health func(id int64) (nodesync.HealthView, bool)
	// Nodes lists the nodes to watch.
	Nodes func(ctx context.Context) ([]Node, error)
	// Notify sends one text to every linked admin.
	Notify func(ctx context.Context, text string)
	// Tick is how often health is sampled; zero: a minute.
	Tick time.Duration
}

// Node is a watched node.
type Node struct {
	ID      int64
	Name    string
	Enabled bool
}

type Watcher struct {
	d      Deps
	mu     sync.Mutex
	states map[nodeKind]*track
}

type nodeKind struct {
	node int64
	kind string // "cpu" or "mem"
}

type track struct {
	breach int       // consecutive breaching ticks
	firing bool      // an alert went out and no recovery since
	sentAt time.Time // last alert, for the cooldown
}

func New(d Deps) *Watcher {
	if d.Tick <= 0 {
		d.Tick = time.Minute
	}
	return &Watcher{d: d, states: map[nodeKind]*track{}}
}

type limits struct {
	on       bool
	cpu      int
	mem      int
	minutes  int
	cooldown time.Duration
}

func (w *Watcher) loadLimits(ctx context.Context) limits {
	var l limits
	l.on, _ = w.d.Settings.On(ctx, Enabled)
	l.cpu = getInt(ctx, w.d.Settings, KeyCPU, DefCPU)
	l.mem = getInt(ctx, w.d.Settings, KeyMem, DefMem)
	l.minutes = getInt(ctx, w.d.Settings, KeyMinutes, DefMinutes)
	if l.minutes < 1 {
		l.minutes = 1
	}
	l.cooldown = time.Duration(getInt(ctx, w.d.Settings, KeyCooldown, DefCooldown)) * time.Hour
	return l
}

func getInt(ctx context.Context, s *settings.Settings, key string, def int) int {
	v, ok, err := settings.Get[int](ctx, s, key)
	if err != nil || !ok {
		return def
	}
	return v
}

// Run samples every tick until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(w.d.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.once(ctx)
		}
	}
}

func (w *Watcher) once(ctx context.Context) {
	lim := w.loadLimits(ctx)
	if !lim.on {
		return
	}
	if w.d.Nodes == nil || w.d.Health == nil || w.d.Notify == nil {
		return
	}
	nodes, err := w.d.Nodes(ctx)
	if err != nil {
		return
	}
	now := w.d.Now()
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		hv, ok := w.d.Health(n.ID)
		if !ok || !hv.OK {
			continue
		}
		if now.Sub(hv.CheckedAt) > 2*w.d.Tick+10*time.Second {
			continue // stale: the node, not the load, is the problem
		}
		sys := hv.Health.System
		memPct := 0.0
		if sys.MemTotal > 0 {
			memPct = 100 * float64(sys.MemUsed) / float64(sys.MemTotal)
		}
		w.check(ctx, now, lim, n, "cpu", sys.CPUPercent, float64(lim.cpu),
			fmt.Sprintf("CPU %.0f%%", sys.CPUPercent))
		w.check(ctx, now, lim, n, "mem", memPct, float64(lim.mem),
			fmt.Sprintf("RAM %.0f%% (%s/%s)", memPct, iec(sys.MemUsed), iec(sys.MemTotal)))
	}
}

// check folds one sample into the breach counter and fires on hold.
func (w *Watcher) check(ctx context.Context, now time.Time, lim limits, n Node, kind string, value, threshold float64, _ string) {
	k := nodeKind{node: n.ID, kind: kind}
	w.mu.Lock()
	t := w.states[k]
	if t == nil {
		t = &track{}
		w.states[k] = t
	}
	if value < threshold {
		if t.firing && value < threshold-Hysteresis {
			t.firing = false
			t.breach = 0
			w.mu.Unlock()
			w.recovered(ctx, n, kind, value)
			return
		}
		t.breach = 0
		w.mu.Unlock()
		return
	}
	t.breach++
	hold := t.breach >= lim.minutes
	cool := now.Sub(t.sentAt) < lim.cooldown
	firing := t.firing
	w.mu.Unlock()
	if !hold || firing || cool {
		return
	}
	if !w.claim(ctx, n.ID, kind, now) {
		return // already sent today (a restart must not resend)
	}
	w.mu.Lock()
	t.firing = true
	t.sentAt = now
	w.mu.Unlock()
	w.fire(ctx, n, kind, value, threshold, now)
}

// claim inserts the day dedup row; false when one is already there.
func (w *Watcher) claim(ctx context.Context, node int64, kind string, now time.Time) bool {
	res, err := w.d.Store.DB.ExecContext(ctx, `INSERT OR IGNORE INTO admin_notices (node_id, kind, period, sent_at) VALUES (?, ?, ?, ?)`,
		node, kind+"_high", now.Unix()/86400, now.Unix())
	if err != nil {
		w.d.Log.Warn("nodealert: dedup not written", "err", err)
		return true
	}
	n, err := res.RowsAffected()
	if err != nil {
		return true
	}
	return n > 0
}

func (w *Watcher) fire(ctx context.Context, n Node, kind string, value, threshold float64, now time.Time) {
	name := n.Name
	if name == "" {
		name = "node-" + itoa(n.ID)
	}
	w.d.Notify(ctx, "⚠️ <b>"+html.EscapeString(name)+"</b>: "+kindName(kind)+" "+pct(value)+" (порог "+pct(threshold)+", "+now.UTC().Format("15:04")+" UTC)")
}

func (w *Watcher) recovered(ctx context.Context, n Node, kind string, value float64) {
	name := n.Name
	if name == "" {
		name = "node-" + itoa(n.ID)
	}
	w.d.Notify(ctx, "✅ <b>"+html.EscapeString(name)+"</b>: "+kindName(kind)+" "+pct(value)+" — норма")
}

func kindName(kind string) string {
	if kind == "cpu" {
		return "CPU"
	}
	return "RAM"
}

func pct(v float64) string {
	return itoa(int64(v+0.5)) + "%"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func iec(n uint64) string {
	const unit = 1024
	if n < unit {
		return itoa(int64(n)) + " B"
	}
	div := uint64(unit)
	exp := 0
	for v := n / unit; v >= unit && exp < 3; v, exp = v/unit, exp+1 {
		div *= unit
	}
	units := []string{"KB", "MB", "GB", "TB"}
	q := float64(n) / float64(div)
	whole := int64(q)
	frac := int64((q - float64(whole)) * 10)
	return itoa(whole) + "." + itoa(frac) + " " + units[exp]
}
