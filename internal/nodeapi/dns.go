package nodeapi

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// RenderAddress renders one DNS server for a mihomo nameserver list: a plain
// IP takes the configured port (53 when unset), encrypted endpoints travel
// verbatim (their port, if any, lives in the URL). A port set on a URL server
// without one is folded into its host: dropping it would send the queries to
// the wrong port while the UI shows the right one.
func RenderAddress(s DNSServer) string {
	if net.ParseIP(s.Address) != nil {
		if s.Port > 0 && s.Port != 53 {
			return net.JoinHostPort(s.Address, strconv.Itoa(s.Port))
		}
		return s.Address
	}
	if s.Port <= 0 || s.Port > 65535 {
		return s.Address
	}
	u, err := url.Parse(s.Address)
	if err != nil || u.Host == "" {
		return s.Address
	}
	if _, _, err := net.SplitHostPort(u.Host); err == nil {
		return s.Address // an explicit port in the URL wins
	}
	switch strings.ToLower(u.Scheme) {
	case "udp", "tcp", "tls", "quic", "https", "http":
		u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(s.Port))
		return u.String()
	}
	return s.Address
}

// FallbackFilterSection maps the panel's poisoning-protection lists to mihomo's
// fallback-filter. mihomo filters a single GEOIP country code, so at most one
// travels (the panel refuses more); everything else goes verbatim. geoip rides
// explicitly even when off: otherwise mihomo seeds a CN filter with geodata
// downloads behind the admin's back.
func FallbackFilterSection(f DNSFallbackFilter) map[string]any {
	out := map[string]any{"ipcidr": f.IPCIDR, "domain": f.Domain, "geosite": f.Geosite}
	if len(f.GeoIP) > 0 {
		out["geoip"] = true
		out["geoip-code"] = f.GeoIP[0]
	} else {
		out["geoip"] = false
	}
	return out
}

// PolicyMatcher normalizes a DNS policy key the way mihomo reads it: a
// "domain:" prefix is how the panel writes a plain suffix, and mihomo only
// knows plain suffixes (its trie would file "domain:example.com" under a label
// no real query ever has). IP networks never match a domain query either and
// are refused by validation, so they never reach here.
func PolicyMatcher(k string) string {
	return strings.TrimPrefix(k, "domain:")
}

// SplitServers divides servers into the default addresses and the policy map
// (matcher → server addresses), the way mihomo reads them: what nothing else
// claims goes to the defaults.
func SplitServers(servers []DNSServer) (defaults []string, policy map[string][]string) {
	policy = map[string][]string{}
	for _, s := range servers {
		addr := RenderAddress(s)
		if len(s.Domains) == 0 {
			defaults = append(defaults, addr)
			continue
		}
		for _, k := range s.Domains {
			m := PolicyMatcher(k)
			policy[m] = append(policy[m], addr)
		}
	}
	return defaults, policy
}
