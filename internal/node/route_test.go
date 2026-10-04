package node

import "testing"

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
