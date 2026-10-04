package node

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/metacubex/mihomo/component/resolver"
	"github.com/miekg/dns"

	"cozy/internal/nodeapi"
)

// stubResolver answers every name with one IP: the egress walk without the net.
type stubResolver struct{ ip netip.Addr }

func (s stubResolver) LookupIP(context.Context, string) ([]netip.Addr, error) {
	return []netip.Addr{s.ip}, nil
}
func (s stubResolver) LookupIPv4(context.Context, string) ([]netip.Addr, error) {
	return []netip.Addr{s.ip}, nil
}
func (s stubResolver) LookupIPv6(context.Context, string) ([]netip.Addr, error) {
	return []netip.Addr{s.ip}, nil
}
func (s stubResolver) ResolveECH(context.Context, string) ([]byte, error) { return nil, nil }
func (s stubResolver) ExchangeContext(context.Context, *dns.Msg) (*dns.Msg, error) {
	return nil, nil
}
func (s stubResolver) Invalid() bool    { return true }
func (s stubResolver) ClearCache()      {}
func (s stubResolver) ResetConnection() {}

func TestEgressDial(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	e := &Engine{}
	if _, err := e.EgressDial(ctx, nodeapi.EgressDialRequest{}); err == nil {
		t.Fatal("empty domain accepted")
	}
	if _, err := e.EgressDial(ctx, nodeapi.EgressDialRequest{Domain: "example.com", Port: 99999}); err == nil {
		t.Fatal("bad port accepted")
	}
	// No resolver before the first Apply: a result naming the cause, not silence.
	if res, err := e.EgressDial(ctx, nodeapi.EgressDialRequest{Domain: "example.com"}); err != nil || res.Error == "" {
		t.Fatalf("not ready: %+v %v", res, err)
	}

	old := resolver.DirectHostResolver
	resolver.DirectHostResolver = stubResolver{ip: netip.MustParseAddr("127.0.0.1")}
	defer func() { resolver.DirectHostResolver = old }()

	// Plain TCP answers the dial but cannot shake TLS hands: IP and RTT land,
	// TLS stays false with the reason.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	res, err := e.EgressDial(ctx, nodeapi.EgressDialRequest{Domain: "example.com", Port: ln.Addr().(*net.TCPAddr).Port})
	if err != nil {
		t.Fatal(err)
	}
	if res.IP != "127.0.0.1" || res.TLS || res.Error == "" {
		t.Fatalf("dial: %+v", res)
	}
}
