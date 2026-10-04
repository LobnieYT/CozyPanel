package node

import (
	"reflect"
	"testing"

	"cozy/internal/nodeapi"
)

func TestDNSSection(t *testing.T) {
	if got := dnsSection(nodeapi.DesiredState{}); !reflect.DeepEqual(got, map[string]any{"enable": false}) {
		t.Fatalf("off: %v", got)
	}
	st := nodeapi.DesiredState{DNS: &nodeapi.NodeDNS{Enable: true, IPv6: true, PreferH3: true,
		Servers: []nodeapi.DNSServer{
			{Address: "1.1.1.1"},
			{Address: "https://xbox-dns.ru/dns-query", Domains: []string{"geosite:google"}, Tag: "xbox"},
		},
		ProxyServers:   []string{"https://1.1.1.1/dns-query"},
		Fallback:       []string{"8.8.8.8"},
		FallbackFilter: nodeapi.DNSFallbackFilter{Geosite: []string{"cn"}},
		Hosts:          map[string][]string{"internal.example": {"10.1.2.3"}},
	}}
	got := dnsSection(st)
	if got["enable"] != true || got["ipv6"] != true || got["prefer-h3"] != true {
		t.Fatalf("flags: %v", got)
	}
	ns, _ := got["nameserver"].([]string)
	if len(ns) != 1 || ns[0] != "1.1.1.1" {
		t.Fatalf("nameserver: %v", got["nameserver"])
	}
	pol, _ := got["nameserver-policy"].(map[string][]string)
	if len(pol["geosite:google"]) != 1 {
		t.Fatalf("policy: %v", got["nameserver-policy"])
	}
	if _, ok := got["fallback"]; !ok {
		t.Fatalf("no fallback: %v", got)
	}
	if hosts := dnsHosts(st); hosts["internal.example"] != "10.1.2.3" {
		t.Fatalf("hosts: %v", hosts)
	}
	if !needsGeo(st) {
		t.Fatal("geosite policy needs geo")
	}
	if needsGeo(nodeapi.DesiredState{}) {
		t.Fatal("empty needs geo")
	}
}
