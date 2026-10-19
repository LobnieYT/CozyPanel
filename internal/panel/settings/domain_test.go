package settings

import "testing"

// A DNS name does not care about the case, and the ACME server brings it to the lower
// one: a domain written with capitals is sent as it is written, the server "changes" it,
// and the client library refuses the order. One spelling, everywhere.
func TestCanonicalDomain(t *testing.T) {
	for in, want := range map[string]string{
		"Alpha-Sun-X2uTG1QW.expresshost.cloud": "alpha-sun-x2utg1qw.expresshost.cloud",
		"VPN.Example.COM":                      "vpn.example.com",
		"  vpn.example.com  ":                  "vpn.example.com",
		"vpn.example.com.":                     "vpn.example.com",
		"VPN.EXAMPLE.COM.":                     "vpn.example.com",
		"":                                     "",
		"   ":                                  "",
		"77.238.243.225":                       "77.238.243.225",
	} {
		if got := CanonicalDomain(in); got != want {
			t.Errorf("CanonicalDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
