package node

import (
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"cozy/internal/nodeapi"
)

// The node shapes the speed itself, in process: a token bucket per slot and direction.
// With the node's fair share on, the channel is divided between the slots that are
// downloading at the moment — every second each gets max(share floor, channel/hungry) —
// so one download cannot take the whole line from the rest.
const (
	mbit     = 1e6 / 8 // Mbit/s in bytes a second
	burstSec = 0.1     // a bucket's burst: a tenth of a second of its own speed
	shareMin = 1       // the least a fair share hands out, Mbit/s: below that, precision
	idleFor  = 3 * time.Second
)

// Shaper holds the channel's width and hands every slot its share once a second.
type Shaper struct {
	mu      sync.Mutex
	channel int  // Mbit/s; 0 = no fair share
	fair    bool // divide the channel between the slots that download
	groups  map[string]*group
	now     func() time.Time
}

type group struct {
	up, down *rate.Limiter // nil: nothing shared or capped
	own      int           // the slot's own cap in Mbit/s, 0 = none
	downAt   time.Time     // when it last read anything (it was downloading)
	conns    map[*shapedConn]struct{}
}

func newShaper(now func() time.Time) *Shaper {
	return &Shaper{groups: map[string]*group{}, now: now}
}

func limiter(mbps int) *rate.Limiter {
	if mbps <= 0 {
		return nil
	}
	perSec := float64(mbps) * mbit
	return rate.NewLimiter(rate.Limit(perSec), max(1, int(perSec*burstSec)))
}

func (s *Shaper) groupLocked(name string) *group {
	g := s.groups[name]
	if g == nil {
		g = &group{conns: map[*shapedConn]struct{}{}}
		s.groups[name] = g
	}
	return g
}

// fairShareLocked is what one hungry slot may use: its own cap, or the smaller of it
// and its share of the channel.
func (s *Shaper) fairShareLocked(g *group) int {
	own := g.own
	if !s.fair || s.channel <= 0 {
		return own
	}
	hungry := 0
	for _, other := range s.groups {
		if other.downAt.After(s.now().Add(-idleFor)) {
			hungry++
		}
	}
	if hungry <= 1 {
		return own
	}
	share := max(s.channel/hungry, shareMin)
	if own > 0 {
		share = min(share, own)
	}
	return share
}

func (s *Shaper) applyLocked(g *group) {
	up, down := limiter(g.own), limiter(g.own)
	if s.fair && s.channel > 0 {
		share := limiter(s.fairShareLocked(g))
		if up == nil || share.Limit() < up.Limit() {
			up = share
		}
		if down == nil || share.Limit() < down.Limit() {
			down = share
		}
	}
	g.up, g.down = up, down
	for c := range g.conns {
		c.setLimits(up, down)
	}
}

// setMbps records a slot's own cap and hands it the new bucket: a connection opened
// under another cap would keep the old one, so the client reconnects.
func (s *Shaper) setMbps(name string, mbps int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groupLocked(name)
	if g.own == mbps && g.up != nil {
		return
	}
	g.own = mbps
	s.applyLocked(g)
}

// shaping sets the node's own channel: a width without fair share ends the sharing.
func (s *Shaper) shaping(sh *nodeapi.Shaping) {
	s.mu.Lock()
	defer s.mu.Unlock()
	channel, fair := 0, false
	if sh != nil {
		channel, fair = sh.ChannelMbps, sh.FairShare
	}
	if channel == s.channel && fair == s.fair {
		return
	}
	s.channel, s.fair = channel, fair
	for _, g := range s.groups {
		s.applyLocked(g)
	}
}

// share gives every slot its share again: once a second, so a new download does not
// wait out the old one's burst.
func (s *Shaper) share() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.groups {
		s.applyLocked(g)
	}
}

// shapedConn is a connection whose read and write wait for the slot's tokens.
type shapedConn struct {
	net.Conn
	s     *Shaper
	name  string
	group *group
	mu    sync.Mutex
	up    *rate.Limiter
	down  *rate.Limiter
}

func (c *shapedConn) setLimits(up, down *rate.Limiter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.up, c.down = up, down
}

// take waits for the tokens of n bytes, in bursts, so a big read does not sit on the
// bucket for seconds at once.
func (c *shapedConn) take(l *rate.Limiter, n int) {
	if l == nil || n <= 0 {
		return
	}
	for n > 0 {
		step := min(n, l.Burst())
		if step <= 0 {
			return
		}
		w := l.ReserveN(time.Now(), step)
		if !w.OK() {
			return
		}
		if d := w.Delay(); d > 0 {
			timer := time.NewTimer(d)
			<-timer.C
		}
		w.Cancel()
		n -= step
	}
}

func (c *shapedConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	down := c.down
	c.mu.Unlock()
	if down != nil {
		c.s.downloading(c.name)
		c.take(down, len(p))
	}
	return c.Conn.Read(p)
}

func (c *shapedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	up := c.up
	c.mu.Unlock()
	c.take(up, len(p))
	return c.Conn.Write(p)
}

// downloading marks the slot as one of those that share the channel.
func (s *Shaper) downloading(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groupLocked(name)
	g.downAt = s.now()
}

// addConn puts a connection under the slot's current limits, or leaves it alone when
// nothing is capped and nothing is shared.
func (s *Shaper) addConn(name string, c net.Conn) (*shapedConn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groupLocked(name)
	if g.up == nil && g.down == nil && g.own == 0 {
		// A slot that just appeared on a shared channel gets its share from the start.
		s.applyLocked(g)
	}
	if g.up == nil && g.down == nil {
		return nil, false
	}
	sc := &shapedConn{Conn: c, s: s, name: name, group: g, up: g.up, down: g.down}
	g.conns[sc] = struct{}{}
	return sc, true
}

func (s *Shaper) dropConn(c *shapedConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(c.group.conns, c)
	if len(c.group.conns) == 0 && c.group.own == 0 && (!s.fair || s.channel == 0) {
		delete(s.groups, c.name)
	}
}

// run recomputes the shares once a second, like the system sampler beside it.
func (s *Shaper) run(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for range t.C {
		s.share()
	}
}
