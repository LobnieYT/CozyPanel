package netcfg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseNodeDNS(t *testing.T) {
	if d, err := ParseNodeDNS(""); err != nil || d != nil {
		t.Fatalf("empty: %+v %v", d, err)
	}
	if d, err := ParseNodeDNS(`{"enable":false}`); err != nil || d == nil || d.Enable {
		t.Fatalf("disabled: %+v %v", d, err)
	}
	good := `{"enable":true,"ipv6":true,"nameservers":["1.1.1.1","https://dns.google/dns-query","tls://8.8.8.8"],
		"proxy_servers":["https://1.1.1.1/dns-query"],"policy":{"geosite:cn":["223.5.5.5"],"domain:example.com":["1.1.1.1"],"10.0.0.0/8":["10.0.0.1"]},
		"hosts":{"internal.example":["10.1.2.3"]}}`
	d, err := ParseNodeDNS(good)
	if err != nil || len(d.Nameservers) != 3 || len(d.Policy) != 3 || d.Hosts["internal.example"][0] != "10.1.2.3" {
		t.Fatalf("good: %+v %v", d, err)
	}
	for _, c := range []struct {
		doc   string
		field string
	}{
		{`{"enable":true}`, "nameservers"},
		{`{"enable":true,"nameservers":["ftp://x/"]}`, "nameservers"},
		{`{"enable":true,"nameservers":["1.1.1.1"],"policy":{"bogus!":[]}}`, "policy"},
		{`{"enable":true,"nameservers":["1.1.1.1"],"policy":{"geosite:cn":["notaip"]}}`, "policy"},
		{`{"enable":true,"nameservers":["1.1.1.1"],"hosts":{"bad..name":["1.1.1.1"]}}`, "hosts"},
		{`{"enable":true,"nameservers":["1.1.1.1"],"hosts":{"x.example":["notip"]}}`, "hosts"},
		{`not json`, "dns"},
	} {
		if _, err := ParseNodeDNS(c.doc); err == nil {
			t.Fatalf("%s: parsed", c.doc)
		} else if fe, ok := err.(*fieldErr); !ok || fe.Field != c.field {
			t.Fatalf("%s: %v, want field %s", c.doc, err, c.field)
		}
	}
}

func TestParseNodeRoutes(t *testing.T) {
	raw := "# comment\n\nDOMAIN-SUFFIX,example.com,DIRECT\nIP-CIDR,10.0.0.0/8,office,no-resolve\nGEOSITE,category-ru,DIRECT\nDST-PORT,25,REJECT\n"
	parsed, err := ParseNodeRoutes(raw)
	if err != nil {
		t.Fatal(err)
	}
	rs := parsed.Rules
	if len(rs) != 4 || !strings.HasSuffix(rs[1].Rule, "no-resolve") {
		t.Fatalf("good: %+v %v", rs, err)
	}
	for _, bad := range []string{
		"DOMAIN,example.com",                    // no target
		"BOGUS,x,DIRECT",                        // bad type
		"IP-CIDR,999.1.1.1,DIRECT",              // bad value
		"DST-PORT,abc,REJECT",                   // bad port
		"DOMAIN,example.com,my proxy",           // space in target
		"DOMAIN,example.com,DIRECT,resolve=yes", // bad flag
	} {
		if _, err := ParseNodeRoutes(bad); err == nil {
			t.Fatalf("%q: parsed", bad)
		}
	}
}

func TestParseNodeOutbounds(t *testing.T) {
	if o, err := ParseNodeOutbounds(""); err != nil || o != nil {
		t.Fatalf("empty: %+v %v", o, err)
	}
	good := `[{"name":"office","type":"wireguard","server":"203.0.113.9","port":51820},{"name":"socks","type":"socks5","server":"127.0.0.1","port":1080}]`
	o, err := ParseNodeOutbounds(good)
	if err != nil || len(o) != 2 || o[0].Name != "office" {
		t.Fatalf("good: %+v %v", o, err)
	}
	for _, c := range []struct {
		doc   string
		field string
	}{
		{`[{"name":"x"}]`, "outbounds"},
		{`[{"name":"DIRECT","type":"socks5"}]`, "outbounds"},
		{`[{"name":"NODE-3","type":"socks5"}]`, "outbounds"},
		{`[{"name":"a","type":"socks5"},{"name":"A","type":"socks5"}]`, "outbounds"},
		{`{"name":"x"}`, "outbounds"},
	} {
		if _, err := ParseNodeOutbounds(c.doc); err == nil {
			t.Fatalf("%s: parsed", c.doc)
		} else if fe, ok := err.(*fieldErr); !ok || fe.Field != c.field {
			t.Fatalf("%s: %v", c.doc, err)
		}
	}
}

func TestParseNodeRoutesDefault(t *testing.T) {
	parsed, err := ParseNodeRoutes("# default: WARP\nDOMAIN,example.com,DIRECT\n")
	if err != nil || parsed.Default != "WARP" || len(parsed.Rules) != 1 {
		t.Fatalf("default: %+v %v", parsed, err)
	}
	if _, err := ParseNodeRoutes("# default: my proxy\n"); err == nil {
		t.Fatal("spaced default parsed")
	}
}

func TestParseSubDNS(t *testing.T) {
	if d, err := ParseSubDNS(""); err != nil || d != nil {
		t.Fatalf("empty: %+v %v", d, err)
	}
	d, err := ParseSubDNS(`{"nameservers":["https://1.1.1.1/dns-query"],"proxy_servers":["https://dns.google/dns-query"],"policy":{"geosite:category-ru":["77.88.8.8"]},"fake_ip":true}`)
	if err != nil || !d.FakeIP || d.Policy["geosite:category-ru"][0] != "77.88.8.8" {
		t.Fatalf("good: %+v %v", d, err)
	}
	if _, err := ParseSubDNS(`{"nameservers":[]}`); err == nil {
		t.Fatal("no servers parsed")
	}
	if _, err := ParseSubDNS(`{"nameservers":["https://1.1.1.1/dns-query"],"policy":{"geosite:cn":["https://x/"]}}`); err == nil {
		t.Fatal("DoH in policy parsed")
	}
}

func TestMatchRoute(t *testing.T) {
	raw := "DOMAIN-SUFFIX,example.com,DIRECT\nIP-CIDR,10.0.0.0/8,office\nDST-PORT,25,REJECT\nGEOIP,ru,WARP\nDOMAIN-REGEX,^ads\\.,REJECT\nDOMAIN-WILDCARD,*.internal,DIRECT\nIN-NAME,vless,office\n"
	rs, err := ParseNodeRoutes(raw)
	if err != nil {
		t.Fatal(err)
	}
	rules := rs.Rules
	for _, c := range []struct {
		in           RouteInput
		rule         string
		target       string
		matched, geo bool
	}{
		{RouteInput{Domain: "a.example.com"}, "DOMAIN-SUFFIX,example.com,DIRECT", "DIRECT", true, false},
		{RouteInput{Domain: "example.com", IP: "10.1.2.3"}, "DOMAIN-SUFFIX,example.com,DIRECT", "DIRECT", true, false},
		{RouteInput{IP: "10.1.2.3"}, "IP-CIDR,10.0.0.0/8,office", "office", true, false},
		{RouteInput{Domain: "x.example", Port: 25}, "DST-PORT,25,REJECT", "REJECT", true, false},
		{RouteInput{Domain: "ads.tracker"}, "DOMAIN-REGEX,^ads\\.,REJECT", "REJECT", true, true},
		{RouteInput{Domain: "a.internal"}, "DOMAIN-WILDCARD,*.internal,DIRECT", "DIRECT", true, true},
		{RouteInput{Domain: "x.io", Inbound: "vless"}, "IN-NAME,vless,office", "office", true, true},
		// GEO rules are skipped dryly: no match, but a live test is advised.
		{RouteInput{Domain: "yandex.ru"}, "", "", false, true},
	} {
		rule, target, matched, geo := MatchRoute(rules, c.in)
		if matched != c.matched || rule != c.rule || target != c.target || geo != c.geo {
			t.Errorf("%+v: %q %q %v %v, want %q %q %v %v", c.in, rule, target, matched, geo, c.rule, c.target, c.matched, c.geo)
		}
	}
	// First match wins; GEOIP is skipped dryly.
	parsed2, _ := ParseNodeRoutes("GEOIP,ru,WARP\nDOMAIN,example.com,DIRECT\n")
	rs2 := parsed2.Rules
	if rule, _, matched, geo := MatchRoute(rs2, RouteInput{Domain: "example.com"}); !matched || !geo || rule != "DOMAIN,example.com,DIRECT" {
		t.Fatalf("order/geo: %q %v %v", rule, matched, geo)
	}
	if _, _, matched, _ := MatchRoute(rs2, RouteInput{Domain: "other.io"}); matched {
		t.Fatal("unmatched matched")
	}
}

func TestWireGuardConf(t *testing.T) {
	conf := "[Interface]\nPrivateKey = AAA=\nAddress = 10.14.0.2/32, fd00::2/128\nMTU = 1420\n\n[Peer]\nPublicKey = BBB=\nAllowedIPs = 0.0.0.0/0\nEndpoint = 203.0.113.9:51820\n"
	o, err := WireGuardConf("nord", conf)
	if err != nil || o.Name != "nord" {
		t.Fatalf("good: %+v %v", o, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(o.Config, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["type"] != "wireguard" || doc["server"] != "203.0.113.9" || doc["mtu"] != float64(1420) {
		t.Fatalf("doc: %v", doc)
	}
	for _, bad := range []string{"[Interface]\nPrivateKey = AAA=\n", "nonsense", "[Peer]\nPublicKey = x\nEndpoint = bad\n"} {
		if _, err := WireGuardConf("x", bad); err == nil {
			t.Fatalf("%q: parsed", bad)
		}
	}
}
