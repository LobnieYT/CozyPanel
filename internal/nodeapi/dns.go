package nodeapi

import (
	"net"
	"strconv"
)

// RenderAddress renders one DNS server for a mihomo nameserver list: a plain
// IP takes the configured port (53 when unset), encrypted endpoints travel
// verbatim (their port, if any, lives in the URL).
func RenderAddress(s DNSServer) string {
	if net.ParseIP(s.Address) != nil && s.Port > 0 && s.Port != 53 {
		return net.JoinHostPort(s.Address, strconv.Itoa(s.Port))
	}
	return s.Address
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
			policy[k] = append(policy[k], addr)
		}
	}
	return defaults, policy
}
