package nodeapi

import "testing"

func TestPolicyMatcher(t *testing.T) {
	if got := PolicyMatcher("domain:example.com"); got != "example.com" {
		t.Fatalf("strip: %q", got)
	}
	if got := PolicyMatcher("geosite:google"); got != "geosite:google" {
		t.Fatalf("geosite: %q", got)
	}
	def, policy := SplitServers([]DNSServer{
		{Address: "1.1.1.1"},
		{Address: "77.88.8.8", Domains: []string{"domain:yandex.ru"}},
	})
	if len(def) != 1 || len(policy["yandex.ru"]) != 1 || len(policy) != 1 {
		t.Fatalf("split: %v %v", def, policy)
	}
}

func TestNormalizeDomain(t *testing.T) {
	for in, want := range map[string]string{
		"https://gemini.google.com/app": "gemini.google.com",
		"http://Example.COM:8443/x?y=1": "example.com",
		"  gemini.google.com. ":         "gemini.google.com",
		"GEMINI.GOOGLE.COM":             "gemini.google.com",
		"example.com/path":              "example.com",
		"":                              "",
		"https://":                      "",
		"not a domain":                  "",
		"gemini.google.com:443":         "gemini.google.com",
	} {
		if got := NormalizeDomain(in); got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderAddress(t *testing.T) {
	for _, tc := range []struct {
		in   DNSServer
		want string
	}{
		{DNSServer{Address: "1.1.1.1"}, "1.1.1.1"},
		{DNSServer{Address: "1.1.1.1", Port: 53}, "1.1.1.1"},
		{DNSServer{Address: "1.1.1.1", Port: 5353}, "1.1.1.1:5353"},
		// A port on a URL server folds into its host; without this the queries
		// would go to the scheme's default port instead.
		{DNSServer{Address: "udp://192.0.2.1", Port: 5353}, "udp://192.0.2.1:5353"},
		{DNSServer{Address: "tls://192.0.2.1", Port: 8853}, "tls://192.0.2.1:8853"},
		{DNSServer{Address: "https://example.com/dns-query", Port: 8443}, "https://example.com:8443/dns-query"},
		// An explicit port in the URL wins over the field.
		{DNSServer{Address: "udp://192.0.2.1:5353", Port: 5354}, "udp://192.0.2.1:5353"},
		{DNSServer{Address: "https://1.1.1.1/dns-query"}, "https://1.1.1.1/dns-query"},
		{DNSServer{Address: "system://"}, "system://"},
		{DNSServer{Address: "dhcp://eth0"}, "dhcp://eth0"},
	} {
		if got := RenderAddress(tc.in); got != tc.want {
			t.Errorf("RenderAddress(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
