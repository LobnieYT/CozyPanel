package acme

import (
	"testing"

	"cozy/internal/panel/settings"
)

// Let's Encrypt refuses a CSR with an IP in the Common Name (badCSR) and issues IP
// certificates only with the shortlived profile; domains stay as they were.
func TestOrder(t *testing.T) {
	for id, want := range map[string]struct {
		profile string
		noCN    bool
	}{
		"77.238.243.225":  {"shortlived", true},
		"2001:db8::1":     {"shortlived", true},
		"vpn.example.com": {"", false},
	} {
		req, noCN := order(id)
		if req.Profile != want.profile || noCN != want.noCN || len(req.Domains) != 1 || req.Domains[0] != id || !req.Bundle {
			t.Errorf("%s: %+v, no CN %v", id, req, noCN)
		}
	}
}

// The order asks for the canonical name: the server brings the name to the lower case
// (RFC 8555 §7.4), and the client library compares the identifiers letter by letter — a
// domain with capitals in it would be refused for a name the server only re-cased.
func TestOrderAsksForTheCanonicalName(t *testing.T) {
	req, noCN := order(settings.CanonicalDomain("Alpha-Sun-X2uTG1QW.expresshost.cloud"))
	if len(req.Domains) != 1 || req.Domains[0] != "alpha-sun-x2utg1qw.expresshost.cloud" {
		t.Fatalf("the order asks for %v", req.Domains)
	}
	if noCN {
		t.Fatal("a domain keeps its Common Name")
	}
}
