package subs

import (
	"strings"
	"testing"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub/executor"

	"cozy/internal/nodeapi"
	"cozy/internal/panel/netcfg"
)

// TestMihomoProfileParses renders client profiles for both routings and the
// representative DNS documents and runs each through mihomo's own parser: a
// profile the parser refuses is a VPN that does not start for the user.
// Documents without GEO references parse hermetically; GEO ones need the
// geodata download and skip when the test env cannot fetch it.
func TestMihomoProfileParses(t *testing.T) {
	C.SetHomeDir(t.TempDir())
	docs := map[string]*netcfg.SubDNS{
		"builtin": nil,
		"doh-only": {Servers: []nodeapi.DNSServer{
			{Address: "https://dns.google/dns-query"},
		}, FakeIP: true},
		"mixed": {Servers: []nodeapi.DNSServer{
			{Address: "77.88.8.8"},
			{Address: "8.8.8.8"},
			{Address: "tls://1.1.1.1"},
			{Address: "udp://192.168.1.1", Port: 5353},
		}, Fallback: []string{"1.1.1.1"}, FakeIP: true},
		"policy-fallback": {Servers: []nodeapi.DNSServer{
			{Address: "1.1.1.1"},
			{Address: "https://xbox-dns.ru/dns-query", Domains: []string{"geosite:google"}},
		}, Fallback: []string{"8.8.8.8"}, FakeIP: true},
		"no-fakeip": {Servers: []nodeapi.DNSServer{
			{Address: "8.8.8.8"},
		}},
	}
	for _, r := range []Routing{RoutingRUDirect, RoutingAll} {
		for name, doc := range docs {
			p := profile(t, "")
			p.DNS = doc
			raw, err := Mihomo(p, Groups{}, r)
			if err != nil {
				t.Fatalf("%s %s: render: %v", r, name, err)
			}
			if _, err := executor.ParseWithBytes(raw); err != nil {
				if strings.Contains(err.Error(), "can't download") {
					t.Skipf("%s %s: no geodata in test env: %v", r, name, err)
				}
				t.Fatalf("%s %s: mihomo refuses the profile: %v", r, name, err)
			}
		}
	}
}
