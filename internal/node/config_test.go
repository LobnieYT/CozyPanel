package node

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/metacubex/mihomo/hub/executor"

	"cozy/internal/nodeapi"
	"cozy/internal/proto"
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
	ff, _ := got["fallback-filter"].(map[string]any)
	if ff["geoip"] != false {
		t.Fatalf("geoip must ride explicitly off, or mihomo seeds a CN filter: %v", ff)
	}
	// Policy-only DNS keeps an explicit empty default list (mihomo would seed
	// foreign DoH otherwise) and maps the single GEOIP code.
	policyOnly := nodeapi.DesiredState{DNS: &nodeapi.NodeDNS{Enable: true,
		Servers:  []nodeapi.DNSServer{{Address: "https://xbox-dns.ru/dns-query", Domains: []string{"geosite:google"}}},
		Fallback: []string{"8.8.8.8"}, FallbackFilter: nodeapi.DNSFallbackFilter{GeoIP: []string{"CN"}}}}
	got = dnsSection(policyOnly)
	if ns, _ := got["nameserver"].([]string); ns == nil || len(ns) != 0 {
		t.Fatalf("policy-only nameserver: %v", got["nameserver"])
	}
	ff, _ = got["fallback-filter"].(map[string]any)
	if ff["geoip"] != true || ff["geoip-code"] != "CN" {
		t.Fatalf("single GEOIP code: %v", ff)
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

// The node sniffs TLS/HTTP/QUIC on every inbound and lets the SNI replace an
// IP destination, so clients resolving outside the VPN DNS still match domain
// rules and the DNS policy (the 3x-ui behavior for the same clients).
func TestSnifferSection(t *testing.T) {
	raw, _, err := buildConfig(nodeapi.DesiredState{}, proto.Cert{}, false)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Sniffer struct {
			Enable       bool     `json:"enable"`
			Sniffing     []string `json:"sniffing"`
			OverrideDest bool     `json:"override-destination"`
		} `json:"sniffer"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Sniffer.Enable || !cfg.Sniffer.OverrideDest || len(cfg.Sniffer.Sniffing) != 3 {
		t.Fatalf("sniffer: %+v", cfg.Sniffer)
	}
	if _, err := executor.ParseWithBytes(raw); err != nil {
		t.Fatalf("mihomo refuses the sniffer section: %v", err)
	}
}

func TestValidateNet(t *testing.T) {
	e := &Engine{}
	good := nodeapi.ValidateNetRequest{
		DNS: &nodeapi.NodeDNS{Enable: true,
			Servers:  []nodeapi.DNSServer{{Address: "1.1.1.1"}, {Address: "https://dns.google/dns-query", Domains: []string{"domain:example.com"}}},
			Fallback: []string{"8.8.8.8"}},
		Routes:    nodeapi.NodeRoutes{Rules: []nodeapi.NodeRoute{{Rule: "DOMAIN-SUFFIX,example.com,DIRECT"}}},
		Outbounds: []nodeapi.NodeOutbound{{Name: "office", Config: json.RawMessage(`{"type":"socks5","server":"127.0.0.1","port":1080}`)}},
	}
	if err := e.ValidateNet(good); err != nil {
		t.Fatalf("good: %v", err)
	}
	badProxy := good
	badProxy.Outbounds = []nodeapi.NodeOutbound{{Name: "office", Config: json.RawMessage(`{"type":"socks5"}`)}}
	if err := e.ValidateNet(badProxy); err == nil {
		t.Fatal("bad proxy accepted")
	}
	badRule := good
	badRule.Routes = nodeapi.NodeRoutes{Rules: []nodeapi.NodeRoute{{Rule: "DOMAIN-SUFFIX,example.com,NOWHERE"}}}
	if err := e.ValidateNet(badRule); err == nil {
		t.Fatal("unknown target accepted")
	}
	if err := e.ValidateNet(nodeapi.ValidateNetRequest{}); err != nil {
		t.Fatalf("empty: %v", err)
	}
}
