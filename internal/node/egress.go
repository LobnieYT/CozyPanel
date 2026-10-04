package node

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/metacubex/mihomo/component/resolver"

	"cozy/internal/nodeapi"
)

// dialTimeout bounds one egress dial: a filtered address must fail fast,
// not hold the whole test.
const dialTimeout = 10 * time.Second

// resolveDirect resolves through the resolver the DIRECT outbound dials with:
// the policy-aware main one (no direct-nameserver is ever configured). Nil
// before the first Apply: the node is not ready to speak for its egress.
func resolveDirect(ctx context.Context, domain string) (netip.Addr, error) {
	r := resolver.DirectHostResolver
	if r == nil {
		return netip.Addr{}, errors.New("node not ready")
	}
	return resolver.ResolveIPWithResolver(ctx, domain, r)
}

// EgressDial walks the DIRECT path for a domain the way the tunnel would:
// resolve through the node's policy-aware resolver, open TCP, and shake TLS
// hands with the domain as SNI. It reports where Gemini-like traffic would
// actually go, beyond what DNS alone says.
func (e *Engine) EgressDial(ctx context.Context, req nodeapi.EgressDialRequest) (nodeapi.EgressDialResult, error) {
	domain := nodeapi.NormalizeDomain(req.Domain)
	if domain == "" {
		return nodeapi.EgressDialResult{}, errors.New("empty domain")
	}
	port := req.Port
	if port == 0 {
		port = 443
	}
	if port < 1 || port > 65535 {
		return nodeapi.EgressDialResult{}, errors.New("bad port")
	}
	out := nodeapi.EgressDialResult{Port: port}
	ip, err := resolveDirect(ctx, domain)
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	out.IP = ip.String()
	dialer := &net.Dialer{Timeout: dialTimeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(out.IP, strconv.Itoa(port)))
	out.RTTMs = time.Since(start).Milliseconds()
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	defer conn.Close()
	tlsConn := tls.Client(conn, &tls.Config{ServerName: domain, MinVersion: tls.VersionTLS12})
	hctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if err := tlsConn.HandshakeContext(hctx); err != nil {
		out.Error = err.Error()
		return out, nil
	}
	_ = tlsConn.Close()
	out.TLS = true
	return out, nil
}
