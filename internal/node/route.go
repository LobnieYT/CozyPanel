package node

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/metacubex/mihomo/component/geodata"
	C "github.com/metacubex/mihomo/constant"
	mrules "github.com/metacubex/mihomo/rules"

	"cozy/internal/nodeapi"
)

// RouteTest matches a synthetic connection against the node's effective rules:
// safety REJECTs, custom routes, exits, WARP and the MATCH default, in order.
func (e *Engine) RouteTest(ctx context.Context, req nodeapi.RouteTestRequest) (nodeapi.RouteTestResult, error) {
	e.mu.Lock()
	st := e.applied
	allowPrivate := e.allowPrivate
	e.mu.Unlock()

	if needsGeo(st) {
		if err := e.ensureGeoData(ctx); err != nil {
			return nodeapi.RouteTestResult{}, err
		}
	}
	md, err := testMetadata(ctx, req)
	if err != nil {
		return nodeapi.RouteTestResult{}, err
	}
	for _, line := range rules(st, allowPrivate) {
		typ, value, target, params, ok := splitNodeRule(line)
		if !ok {
			continue
		}
		if typ == "MATCH" {
			return nodeapi.RouteTestResult{Matched: true, Rule: line, Target: target}, nil
		}
		rule, err := mrules.ParseRule(typ, value, target, params, nil)
		if err != nil {
			continue
		}
		matched, _ := rule.Match(md, C.RuleMatchHelper{
			ResolveIP: func() {
				if md.DstIP.IsValid() || md.Host == "" {
					return
				}
				rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				if addrs, err := net.DefaultResolver.LookupIPAddr(rctx, md.Host); err == nil && len(addrs) > 0 {
					if ip, err := netip.ParseAddr(addrs[0].String()); err == nil {
						md.DstIP = ip
					}
				}
			},
			FindProcess:   func() {},
			CheckPassRule: func(string) bool { return false },
		})
		if matched {
			return nodeapi.RouteTestResult{Matched: true, Rule: line, Target: target}, nil
		}
	}
	return nodeapi.RouteTestResult{}, nil
}

// DNSMatch says which of the node's effective DNS servers a domain would be
// asked of: the first policy whose matcher claims it, or the defaults when
// none does. Keys run sorted, the order mihomo reads them in (the policy map
// marshals sorted and mihomo keeps the order). GEO matchers read the node's
// geodata, fetched first like RouteTest does.
func (e *Engine) DNSMatch(ctx context.Context, req nodeapi.DNSMatchRequest) (nodeapi.DNSMatchResult, error) {
	e.mu.Lock()
	st := e.applied
	e.mu.Unlock()
	if st.DNS == nil || !st.DNS.Enable {
		return nodeapi.DNSMatchResult{}, nil
	}
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(req.Domain), "."))
	if domain == "" {
		return nodeapi.DNSMatchResult{}, errors.New("empty domain")
	}
	if needsGeo(st) {
		if err := e.ensureGeoData(ctx); err != nil {
			return nodeapi.DNSMatchResult{}, err
		}
	}
	defaults, policy := nodeapi.SplitServers(st.DNS.Servers)
	keys := make([]string, 0, len(policy))
	for k := range policy {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	running := nodeapi.DNSMatchResult{Enabled: true, Keys: keys}
	for _, k := range keys {
		if name, ok := strings.CutPrefix(k, "geosite:"); ok {
			m, err := geodata.LoadGeoSiteMatcher(name)
			if err != nil {
				// A policy the node cannot load is an error, not a miss:
				// failing quiet into default hides a broken geodata.
				return nodeapi.DNSMatchResult{}, err
			}
			if m.ApplyDomain(domain) {
				running.Matched, running.Key, running.Servers = true, k, policy[k]
				return running, nil
			}
			continue
		}
		suffix := strings.ToLower(strings.TrimPrefix(k, "."))
		if suffix != "" && (domain == suffix || strings.HasSuffix(domain, "."+suffix)) {
			running.Matched, running.Key, running.Servers = true, k, policy[k]
			return running, nil
		}
	}
	if len(defaults) == 0 {
		return running, nil
	}
	running.Matched, running.Key, running.Servers = true, "default", defaults
	return running, nil
}

// testMetadata builds the metadata a connection to (domain or ip):(port) would
// carry. A domain is resolved so IP rules match too; when it does not resolve,
// domain rules still match.
func testMetadata(ctx context.Context, req nodeapi.RouteTestRequest) (*C.Metadata, error) {
	md := &C.Metadata{NetWork: C.TCP, DstPort: uint16(max(req.Port, 0)), Host: req.Domain, InName: req.Inbound}
	if strings.EqualFold(req.Network, "udp") {
		md.NetWork = C.UDP
	}
	if req.IP != "" {
		addr, err := netip.ParseAddr(req.IP)
		if err != nil {
			return nil, err
		}
		md.DstIP = addr
		if req.Domain == "" {
			md.Host = req.IP
		}
	} else if req.Domain != "" {
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if addrs, err := net.DefaultResolver.LookupIPAddr(rctx, req.Domain); err == nil && len(addrs) > 0 {
			if ip, err := netip.ParseAddr(addrs[0].String()); err == nil {
				md.DstIP = ip
			}
		}
	}
	return md, nil
}

// ensureGeoData lives in geo.go (shared with the manual update).
