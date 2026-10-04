package nodeapi

import "testing"

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
