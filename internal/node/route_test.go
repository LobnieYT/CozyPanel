package node

import (
	"context"
	"testing"

	"cozy/internal/nodeapi"
)

// splitNodeRule never panics, whatever the state carries: a 2-part MATCH
// matches everything, anything else malformed reports ok=false.
func TestSplitNodeRule(t *testing.T) {
	typ, _, target, _, ok := splitNodeRule("MATCH,DIRECT")
	if !ok || typ != "MATCH" || target != "DIRECT" {
		t.Fatalf("match: %q %q %v", typ, target, ok)
	}
	typ, value, target, params, ok := splitNodeRule("DOMAIN-SUFFIX,example.com,DIRECT,no-resolve")
	if !ok || typ != "DOMAIN-SUFFIX" || value != "example.com" || target != "DIRECT" || len(params) != 1 {
		t.Fatalf("full: %q %q %q %v %v", typ, value, target, params, ok)
	}
	for _, bad := range []string{"", "MATCH", "DOMAIN,example.com", "a,b,c,d,e"} {
		if _, _, _, _, ok := splitNodeRule(bad); ok {
			t.Fatalf("%q: parsed", bad)
		}
	}
}

// DNSMatch answers from the effective state without traffic: the first policy
// claiming the domain, or the defaults. No GEO here, so no downloads.
func TestDNSMatch(t *testing.T) {
	e := &Engine{applied: nodeapi.DesiredState{DNS: &nodeapi.NodeDNS{Enable: true,
		Servers: []nodeapi.DNSServer{
			{Address: "1.1.1.1"},
			{Address: "77.88.8.8", Domains: []string{"domain:yandex.ru"}},
		}}}}
	r, err := e.DNSMatch(context.Background(), nodeapi.DNSMatchRequest{Domain: "mail.yandex.ru"})
	if err != nil || !r.Matched || r.Key != "yandex.ru" || len(r.Servers) != 1 {
		t.Fatalf("suffix: %+v %v", r, err)
	}
	r, err = e.DNSMatch(context.Background(), nodeapi.DNSMatchRequest{Domain: "example.com"})
	if err != nil || !r.Matched || r.Key != "default" || len(r.Servers) != 1 {
		t.Fatalf("default: %+v %v", r, err)
	}
	if _, err := e.DNSMatch(context.Background(), nodeapi.DNSMatchRequest{}); err == nil {
		t.Fatal("empty domain accepted")
	}
	e.applied.DNS.Enable = false
	if r, err := e.DNSMatch(context.Background(), nodeapi.DNSMatchRequest{Domain: "example.com"}); err != nil || r.Matched {
		t.Fatalf("disabled: %+v %v", r, err)
	}
}
