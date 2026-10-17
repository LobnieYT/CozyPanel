package node

import (
	"bytes"
	"net"
	"testing"
	"time"

	"cozy/internal/nodeapi"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	return NewRegistry("e", 1, time.Minute, func() time.Time { return now })
}

// A slot's own cap shapes its connection; another slot with no cap passes untouched.
func TestShaperCapsByOwnLimit(t *testing.T) {
	r := testRegistry(t)
	r.SetPolicies("e", []nodeapi.Policy{{Slot: "s000001", Allowed: true, Mbps: 1}})
	one := limiter(1)
	if one.Limit() != mbit {
		t.Fatalf("one Mbit/s bucket: %v", one.Limit())
	}
	sc, ok := r.shaper.addConn("s000001", &nopConn{})
	if !ok {
		t.Fatal("a capped slot must shape")
	}
	sc.mu.Lock()
	up, down := sc.up, sc.down
	sc.mu.Unlock()
	if up == nil || down == nil {
		t.Fatal("no buckets on the connection")
	}
	if _, ok := r.shaper.addConn("s000002", &nopConn{}); ok {
		t.Fatal("an uncapped slot must pass untouched")
	}
	r.shaper.dropConn(sc)
	if _, ok := r.shaper.addConn("s000001", &nopConn{}); !ok {
		t.Fatal("a capped slot must shape again after its connection left")
	}
}

// Fair share: two slots downloading on a 10 Mbit channel get five each, and the cap of
// one of them is the smaller of the two.
func TestShaperFairShare(t *testing.T) {
	r := testRegistry(t)
	r.SetShaping(&nodeapi.Shaping{ChannelMbps: 10, FairShare: true})
	r.SetPolicies("e", []nodeapi.Policy{
		{Slot: "s000001", Allowed: true, Mbps: 100},
		{Slot: "s000002", Allowed: true, Mbps: 100},
	})
	now := time.Unix(1_800_000_000, 0)
	r.shaper.mu.Lock()
	r.shaper.now = func() time.Time { return now }
	r.shaper.mu.Unlock()
	a, _ := r.shaper.addConn("s000001", &nopConn{})
	b, _ := r.shaper.addConn("s000002", &nopConn{})
	if a == nil || b == nil {
		t.Fatal("a shared channel must shape both")
	}
	// Both hungry: each gets half the channel.
	r.shaper.downloading("s000001")
	r.shaper.downloading("s000002")
	r.shaper.share()
	r.shaper.share()
	r.shaper.mu.Lock()
	got5 := r.shaper.fairShareLocked(r.shaper.groups["s000001"])
	r.shaper.mu.Unlock()
	if got5 != 5 {
		t.Fatalf("the share of two: %d Mbit/s, want 5", got5)
	}
	// A third joins: three ways.
	c, _ := r.shaper.addConn("s000003", &nopConn{})
	if c == nil {
		t.Fatal("a shared channel shapes every slot")
	}
	r.shaper.mu.Lock()
	r.shaper.groups["s000003"].downAt = now
	r.shaper.mu.Unlock()
	r.shaper.share()
	r.shaper.mu.Lock()
	got3 := r.shaper.fairShareLocked(r.shaper.groups["s000001"])
	r.shaper.mu.Unlock()
	if got3 != 3 {
		t.Fatalf("the share of three: %d Mbit/s, want 3", got3)
	}
	// The cap of a slot stays the ceiling: one slot capped at 2 gets 2, not 3.
	r.SetPolicies("e", []nodeapi.Policy{{Slot: "s000001", Allowed: true, Mbps: 2}})
	r.shaper.mu.Lock()
	capped := r.shaper.fairShareLocked(r.shaper.groups["s000001"])
	r.shaper.mu.Unlock()
	if capped != 2 {
		t.Fatalf("the cap over the share: %d Mbit/s, want 2", capped)
	}
	// A channel width alone divides nothing: one hungry slot keeps its cap.
	r.SetShaping(&nodeapi.Shaping{ChannelMbps: 10})
	r.shaper.mu.Lock()
	alone := r.shaper.fairShareLocked(r.shaper.groups["s000001"])
	r.shaper.mu.Unlock()
	if alone != 2 {
		t.Fatalf("no fair share: %d Mbit/s, want the cap 2", alone)
	}
	_ = b
}

// The bytes a connection passes wait for the bucket: a slow cap makes a read slow, an
// uncapped one does not.
func TestShapedConnWaits(t *testing.T) {
	r := testRegistry(t)
	r.SetPolicies("e", []nodeapi.Policy{{Slot: "s000001", Allowed: true, Mbps: 1}})
	slow, ok := r.shaper.addConn("s000001", &bytesConn{data: bytes.Repeat([]byte("x"), 64<<10)})
	if !ok {
		t.Fatal("a capped slot must shape")
	}
	start := time.Now()
	buf := make([]byte, 32<<10)
	if _, err := slow.Read(buf); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < 100*time.Millisecond {
		t.Fatalf("32 KB at 1 Mbit/s took %s: the bucket does not wait", took)
	}
	// An uncapped slot gets no wrapper: its connection is read as it is.
	plain := &bytesConn{data: bytes.Repeat([]byte("x"), 32<<10)}
	if _, ok := r.shaper.addConn("s000002", plain); ok {
		t.Fatal("an uncapped slot must pass untouched")
	}
	start = time.Now()
	if _, err := plain.Read(buf); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Fatalf("an uncapped read took %s", took)
	}
}

// nopConn is a connection that neither reads nor writes.
type nopConn struct{}

func (*nopConn) Read([]byte) (int, error)         { return 0, nil }
func (*nopConn) Write(p []byte) (int, error)      { return len(p), nil }
func (*nopConn) Close() error                     { return nil }
func (*nopConn) LocalAddr() net.Addr              { return nil }
func (*nopConn) RemoteAddr() net.Addr             { return nil }
func (*nopConn) SetDeadline(time.Time) error      { return nil }
func (*nopConn) SetReadDeadline(time.Time) error  { return nil }
func (*nopConn) SetWriteDeadline(time.Time) error { return nil }

// bytesConn hands out its bytes once.
type bytesConn struct {
	nopConn
	data []byte
	off  int
}

func (c *bytesConn) Read(p []byte) (int, error) {
	if c.off >= len(c.data) {
		return 0, net.ErrClosed
	}
	n := copy(p, c.data[c.off:])
	c.off += n
	return n, nil
}
