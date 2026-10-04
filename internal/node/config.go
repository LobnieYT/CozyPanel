package node

import (
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/metacubex/mihomo/listener"

	"cozy/internal/geox"
	"cozy/internal/nodeapi"
	"cozy/internal/proto"
)

// Traffic to the node's own networks is refused: without these rules any VPN user could
// reach the host's loopback services or the cloud metadata endpoint (169.254.169.254).
// IP rules without no-resolve also catch domains that resolve to private addresses.
var privateRules = []string{
	"IP-CIDR,0.0.0.0/8,REJECT",
	"IP-CIDR,10.0.0.0/8,REJECT",
	"IP-CIDR,100.64.0.0/10,REJECT",
	"IP-CIDR,127.0.0.0/8,REJECT",
	"IP-CIDR,169.254.0.0/16,REJECT",
	"IP-CIDR,172.16.0.0/12,REJECT",
	"IP-CIDR,192.168.0.0/16,REJECT",
	"IP-CIDR,224.0.0.0/3,REJECT",
	"IP-CIDR6,::1/128,REJECT",
	"IP-CIDR6,fc00::/7,REJECT",
	"IP-CIDR6,fe80::/10,REJECT",
}

func rules(st nodeapi.DesiredState, allowPrivate bool) []string {
	var r []string
	if !allowPrivate {
		r = append(r, privateRules...)
	}
	// Outbound SMTP from a shared VPN IP gets the address blacklisted within hours.
	r = append(r, "DST-PORT,25,REJECT")
	r = append(r, exitRules(st)...)
	r = append(r, warpRules(st)...)
	// The admin's own rules come last; the MATCH target is their default outbound.
	// Safety REJECTs above always win, and anything unmatched leaves by the default.
	for _, route := range st.Routes.Rules {
		r = append(r, route.Rule)
	}
	// AdBlock after the admin's rules: explicit routes win over the automatic
	// block, and blocked ads never reach the exits or WARP below.
	r = append(r, nodeapi.AdBlockRules(st.AdBlock)...)
	match := "DIRECT"
	if d := st.Routes.Default; d != "" {
		match = d
	}
	return append(r, "MATCH,"+match)
}

// outbounds are the proxies besides DIRECT: WARP, the other nodes used as exits,
// and the admin's own.
func outbounds(st nodeapi.DesiredState) ([]any, error) {
	out := []any{}
	if st.Warp != nil {
		p, err := warpProxyConfig(st.Warp)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	for _, e := range st.Exits {
		p, err := exitProxyConfig(e)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	for _, o := range st.Outbounds {
		var p map[string]any
		if err := json.Unmarshal(o.Config, &p); err != nil {
			return nil, err
		}
		p["name"] = o.Name
		out = append(out, p)
	}
	return out, nil
}

// buildConfig renders the mihomo config as JSON, which mihomo's YAML parser accepts.
// log-level warning keeps per-connection lines out of mihomo's own output; the ones that
// still come through are dropped by pumpLogs.
//
// A listener that does not pass the checks is left out and reported in rejected (not OK,
// with the reason): one broken inbound must not stop the node from taking every other
// change of the state, new users and policies included. What the whole state needs, the
// outbounds, is an error as before.
func buildConfig(st nodeapi.DesiredState, cert proto.Cert, allowPrivate bool) (raw []byte, rejected []nodeapi.ListenerStatus, err error) {
	listeners := make([]map[string]any, 0, len(st.Inbounds)+1)
	keep := func(name string, l map[string]any, err error) {
		if err == nil {
			// The parser mihomo applies the config with; it opens nothing.
			_, err = listener.ParseListener(l)
		}
		if err != nil {
			rejected = append(rejected, nodeapi.ListenerStatus{Name: name, Error: err.Error()})
			return
		}
		listeners = append(listeners, l)
	}
	for _, in := range st.Inbounds {
		l, err := listenerFor(in, st.Slots, cert, proto.Options{SelfStealPort: st.SelfStealPort})
		keep(in.Name, l, err)
	}
	if st.Relay != nil {
		l, err := relayListener(st.Relay, cert)
		keep(nodeapi.RelayListener, l, err)
	}
	proxies, err := outbounds(st)
	if err != nil {
		return nil, nil, err
	}
	cfg := map[string]any{
		"mode":              "rule",
		"log-level":         "warning",
		"ipv6":              true,
		"allow-lan":         false,
		"mixed-port":        0,
		"find-process-mode": "off",
		"profile":           map[string]any{"store-selected": false, "store-fake-ip": false},
		"dns":               dnsSection(st),
		"proxies":           proxies,
		"rules":             rules(st, allowPrivate),
		"listeners":         listeners,
	}
	if hosts := dnsHosts(st); len(hosts) > 0 {
		cfg["hosts"] = hosts
	}
	if needsGeo(st) {
		// GEO rules and geosite DNS policies read MetaCubeX data: mihomo
		// downloads it into its home itself and refreshes it on its own.
		cfg["geodata-mode"] = true
		cfg["geo-auto-update"] = true
		cfg["geox-url"] = geox.URL
	}
	raw, err = json.Marshal(cfg)
	return raw, rejected, err
}

// needsGeo says whether the state references GEO data: GEO rules, a DNS policy
// on a geosite, or a fallback filter on GEO lists. GEOIP,LAN is built in and
// needs no files.
func needsGeo(st nodeapi.DesiredState) bool {
	for _, r := range st.Routes.Rules {
		up := strings.ToUpper(strings.TrimSpace(r.Rule))
		if strings.HasPrefix(up, "GEOIP,LAN,") || strings.HasPrefix(up, "GEOIP,LAN ") || up == "GEOIP,LAN" {
			continue
		}
		for _, p := range []string{"GEOIP,", "GEOSITE,", "SRC-GEOIP,"} {
			if strings.HasPrefix(up, p) {
				return true
			}
		}
	}
	if st.DNS != nil {
		for _, s := range st.DNS.Servers {
			for _, k := range s.Domains {
				if strings.HasPrefix(strings.ToLower(k), "geosite:") {
					return true
				}
			}
		}
		if len(st.DNS.FallbackFilter.Geosite) > 0 || len(st.DNS.FallbackFilter.GeoIP) > 0 {
			return true
		}
	}
	if st.AdBlock != nil && st.AdBlock.Enabled {
		return true
	}
	return false
}

// checkRuleTargets refuses rules (and the MATCH default) that name no proxy.
// mihomo resolves target names lazily, so a typo would pass its parser and
// kill the matching traffic at runtime instead of failing the save: refuse it
// here, before it replaces a working config. exact lists WARP and the exits
// only when the full state is known (ValidateNet, Apply); the panel allows the
// WARP and NODE-<id> shapes when it checks without the node's state.
func checkRuleTargets(st nodeapi.DesiredState, exact bool) error {
	allow := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true}
	if !exact || st.Warp != nil {
		allow[warpProxy] = true
	}
	for _, x := range st.Exits {
		allow[x.Name] = true
	}
	for _, o := range st.Outbounds {
		allow[o.Name] = true
	}
	known := func(name string) bool {
		if allow[name] {
			return true
		}
		return !exact && (name == warpProxy || strings.HasPrefix(name, "NODE-"))
	}
	if d := st.Routes.Default; d != "" && !known(d) {
		return errors.New("unknown default target " + strconv.Quote(d))
	}
	if st.Routes.Default != "" {
		allow[st.Routes.Default] = true
	}
	for _, r := range st.Routes.Rules {
		_, _, target, _, ok := splitNodeRule(r.Rule)
		if !ok {
			return errors.New("bad rule " + strconv.Quote(r.Rule))
		}
		if !known(target) && !allow[target] {
			return errors.New("unknown target " + strconv.Quote(target) + " in " + strconv.Quote(r.Rule))
		}
	}
	return nil
}

// dnsSection renders the mihomo dns section. Without an enabled admin DNS the
// resolver stays off and the system resolver answers, as before. What no other
// server claims goes to the servers without matchers: the default DNS.
func dnsSection(st nodeapi.DesiredState) map[string]any {
	d := st.DNS
	if d == nil || !d.Enable {
		return map[string]any{"enable": false}
	}
	defaults, policy := nodeapi.SplitServers(d.Servers)
	out := map[string]any{"enable": true, "ipv6": d.IPv6}
	if d.PreferH3 {
		out["prefer-h3"] = true
	}
	if d.UseSystemHosts {
		out["use-system-hosts"] = true
	}
	if len(defaults) > 0 {
		out["nameserver"] = defaults
	} else {
		// mihomo seeds foreign DoH as the default resolvers when the list is
		// absent: an explicit empty list keeps policy-only DNS on its policy
		// and fallback instead of leaking queries.
		out["nameserver"] = []string{}
	}
	// DoH endpoints resolve through these plain servers first: without them a
	// DNS made only of hostnames cannot bootstrap itself.
	plain := []string{}
	for _, s := range append(append([]string{}, defaults...), d.Fallback...) {
		if host, _, err := net.SplitHostPort(s); err == nil {
			s = host
		}
		if ip := net.ParseIP(s); ip != nil && !ip.IsLoopback() {
			plain = append(plain, ip.String())
		}
	}
	if len(plain) == 0 {
		// Yandex first: from Russia it answers directly while 1.1.1.1/8.8.8.8
		// are throttled or poisoned, and elsewhere it is a correct anycast
		// recursive. A poisoned answer fails closed (DoH TLS mismatch) and the
		// next server is tried.
		plain = []string{"77.88.8.8", "1.1.1.1", "8.8.8.8"}
	}
	out["default-nameserver"] = plain
	if len(d.ProxyServers) > 0 {
		out["proxy-server-nameserver"] = d.ProxyServers
	}
	if len(policy) > 0 {
		out["nameserver-policy"] = policy
	}
	if len(d.Fallback) > 0 {
		out["fallback"] = d.Fallback
		out["fallback-filter"] = nodeapi.FallbackFilterSection(d.FallbackFilter)
	}
	return out
}

// dnsHosts renders the top-level hosts map (mihomo keeps hosts outside dns).
func dnsHosts(st nodeapi.DesiredState) map[string]any {
	d := st.DNS
	if d == nil || !d.Enable || len(d.Hosts) == 0 {
		return nil
	}
	hosts := map[string]any{}
	for k, vs := range d.Hosts {
		if len(vs) == 1 {
			hosts[k] = vs[0]
		} else {
			hosts[k] = vs
		}
	}
	return hosts
}

func listenerFor(in nodeapi.Inbound, slots []nodeapi.Slot, cert proto.Cert, o proto.Options) (map[string]any, error) {
	t, err := template(in)
	if err != nil {
		return nil, err
	}
	return proto.Listener(t, in.Name, in.Listen, in.Port, slots, cert, o)
}

// sharedListeners are the inbounds with one key for everyone.
func sharedListeners(st nodeapi.DesiredState) []string {
	var out []string
	for _, in := range st.Inbounds {
		if t, err := template(in); err == nil && proto.Shared(t.Type()) {
			out = append(out, in.Name)
		}
	}
	return out
}

// template reads the inbound's listener template; states saved by cozy ≤ 0.1.2 carry
// a preset with its settings instead.
func template(in nodeapi.Inbound) (proto.Template, error) {
	if len(in.Config) > 0 {
		return proto.FromJSON(in.Config)
	}
	return proto.FromPreset(in.Preset, in.Settings)
}
