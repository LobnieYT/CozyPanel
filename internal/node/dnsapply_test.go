package node

import (
	"testing"

	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/hub/executor"

	"cozy/internal/nodeapi"
	"cozy/internal/proto"
)

// applyNetGlobals must wire what the testers assume: after it, dials resolve
// through the policy-aware resolver instead of the system one.
func TestApplyNetGlobals(t *testing.T) {
	oldResolver, oldMapper, oldService := resolver.DefaultResolver, resolver.DefaultHostMapper, resolver.DefaultService
	oldProxy, oldDirect := resolver.ProxyServerHostResolver, resolver.DirectHostResolver
	oldIPv6 := resolver.DisableIPv6
	defer func() {
		resolver.DefaultResolver, resolver.DefaultHostMapper, resolver.DefaultService = oldResolver, oldMapper, oldService
		resolver.ProxyServerHostResolver, resolver.DirectHostResolver = oldProxy, oldDirect
		resolver.DisableIPv6 = oldIPv6
	}()

	st := nodeapi.DesiredState{DNS: &nodeapi.NodeDNS{Enable: true,
		Servers: []nodeapi.DNSServer{
			{Address: "1.1.1.1"},
			{Address: "https://xbox-dns.ru/dns-query", Domains: []string{"domain:example.com"}},
		},
		Hosts: map[string][]string{"internal.example": {"10.1.2.3"}},
	}}
	raw, _, err := buildConfig(st, proto.Cert{}, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := executor.ParseWithBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	applyNetGlobals(cfg, false)
	if resolver.DirectHostResolver == nil || resolver.DefaultResolver == nil {
		t.Fatal("resolvers left nil: dials would use the system resolver")
	}
	if !resolver.DisableIPv6 {
		t.Fatal("egress IPv6 off must disable IPv6 resolving and dialing")
	}
	if v, ok := resolver.DefaultHosts.Search("internal.example", false); !ok || len(v.IPs) == 0 {
		t.Fatalf("hosts not applied: %+v %v", v, ok)
	}

	off, _, err := buildConfig(nodeapi.DesiredState{}, proto.Cert{}, false)
	if err != nil {
		t.Fatal(err)
	}
	offCfg, err := executor.ParseWithBytes(off)
	if err != nil {
		t.Fatal(err)
	}
	applyNetGlobals(offCfg, false)
	if resolver.DirectHostResolver != nil || resolver.DefaultResolver != nil {
		t.Fatal("disabled DNS must nil the resolvers, like stock")
	}

	applyNetGlobals(cfg, true)
	if resolver.DisableIPv6 {
		t.Fatal("egress IPv6 on must keep IPv6 resolving and dialing")
	}
}
