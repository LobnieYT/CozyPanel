// Package subs renders subscriptions: share links for Xray/sing-box based clients and a
// mihomo profile for Clash-family clients.
package subs

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"cozy/internal/geox"
	"cozy/internal/nodeapi"
	"cozy/internal/panel/domain"
	"cozy/internal/panel/netcfg"
	"cozy/internal/panel/store/db"
	"cozy/internal/proto"
)

// Endpoint describes how clients reach one node.
type Endpoint struct {
	Host      string // IP or domain the client connects to
	SNI       string // TLS server name for Hysteria2/TUIC; empty for IP-only nodes
	PinSHA256 string // hex SHA-256 of the node's self-signed Hysteria2/TUIC certificate
}

// Node is one server in the subscription.
type Node struct {
	ID int64
	// Name is the node's country group, e.g. "🇳🇱 Нидерланды"; with several nodes its
	// leading flag (or the whole name) prefixes proxy names.
	Name     string
	Endpoint Endpoint
}

// Groups are the proxy-group names Clash-family apps show; the admin can rename them.
type Groups struct {
	Main string // selector, "VPN" by default
	Auto string // url-test, "Авто" by default, "Auto" on a panel in English
}

// AliasGroup is the name Clash apps assume for the main group in the rules they inject
// themselves (Koala Clash per-app routing: "PROCESS-NAME,app.exe,PROXY").
const AliasGroup = "PROXY"

// Routing is how Clash-family apps split traffic between the tunnel and the direct path.
type Routing string

const (
	// RoutingRUDirect sends Russian sites and IPs past the tunnel by mihomo's own geodata
	// (MetaCubeX geosite category-ru and GeoIP ru): banks and state services refuse
	// foreign IPs, and the node does not carry traffic that needs no VPN.
	RoutingRUDirect Routing = "ru_direct"
	// RoutingAll sends everything but the LAN through the tunnel and needs no geodata.
	RoutingAll     Routing = "all"
	DefaultRouting         = RoutingRUDirect
)

// ParseRouting maps a stored setting to a mode; empty and unknown values get the default.
func ParseRouting(s string) Routing {
	if r := Routing(s); r == RoutingRUDirect || r == RoutingAll {
		return r
	}
	return DefaultRouting
}

// geoxURL is the geodata download map client profiles point at.
var geoxURL = geox.URL

// WithDefaults fills in the names the admin left empty, in the panel's default language
// ("en", Russian otherwise).
func (g Groups) WithDefaults(lang string) Groups {
	if g.Main == "" {
		g.Main = "VPN"
	}
	if g.Auto == "" {
		g.Auto = "Авто"
		if lang == "en" {
			g.Auto = "Auto"
		}
	}
	return g
}

// reserved names are mihomo's built-in policies; a group or proxy with such a name
// breaks the profile.
var reserved = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL"}

// ValidName checks a group or proxy name the admin typed. Commas are refused because
// rules reference groups as "MATCH,<name>".
func ValidName(s string) error {
	switch {
	case s == "" || strings.TrimSpace(s) != s:
		return errors.New("name_blank")
	case utf8.RuneCountInString(s) > 48:
		return errors.New("name_too_long")
	case strings.ContainsAny(s, ",\"'\\"):
		return errors.New("name_bad_char")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return errors.New("name_bad_char")
		}
	}
	for _, x := range reserved {
		if strings.EqualFold(s, x) {
			return errors.New("name_reserved")
		}
	}
	return nil
}

// ErrNoProxies: nothing of the profile is left for the app, or for any app.
var ErrNoProxies = errors.New("no proxies")

type Profile struct {
	// Skip, when set, hears of an inbound that is left out because it cannot be rendered
	// (a port that is no number, a template that no longer parses): one broken inbound must
	// not empty or fail the subscription of everyone.
	Skip     func(in db.Inbound, err error)
	Slot     db.Slot
	Inbounds []db.Inbound // enabled and allowed for this user, in display order
	Nodes    []Node       // enabled nodes in display order; inbounds of other nodes are skipped
	Direct   []string     // hosts that bypass the tunnel: the panel and every node
	// Fingerprint is the panel's default uTLS profile (proto.ClientInput.Fingerprint).
	Fingerprint string
	// Rules are the admin's own Clash rules (ServedRules), before the built-in routing.
	Rules []string
	// DNS is the admin's subscription DNS (sub_dns); nil: the built-in profile DNS.
	DNS *netcfg.SubDNS
	// AdBlock is connection-level ad blocking (sub_adblock); nil: off.
	AdBlock *nodeapi.AdBlock
}

type proxy struct {
	name    string
	node    int64
	inbound int64
	uri     string
	yaml    map[string]any
}

// NodePrefix is what precedes proxy names of a node when a subscription has several:
// the flag a name starts with, or the whole name.
func NodePrefix(name string) string {
	rs := []rune(strings.TrimSpace(name))
	if len(rs) >= 2 && isRegional(rs[0]) && isRegional(rs[1]) {
		return string(rs[:2])
	}
	return string(rs)
}

func isRegional(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// build renders the user's proxies node by node, in the nodes' order.
func build(p Profile) ([]proxy, error) {
	var out []proxy
	used := map[string]bool{}
	slot := proto.Slot{Name: p.Slot.Name, UUID: p.Slot.Uuid, Secret: p.Slot.Secret}
	multi := len(p.Nodes) > 1
	for _, n := range p.Nodes {
		for _, in := range p.Inbounds {
			if in.NodeID != n.ID {
				continue
			}
			port, err := firstPort(in.Port)
			if err != nil {
				p.skip(in, err)
				continue
			}
			t, err := parseTemplate(in.Config)
			if err != nil {
				// Saved configs are validated; one broken inbound must not empty the subscription.
				p.skip(in, err)
				continue
			}
			base := domain.ProxyName(in)
			// A name the admin typed is used as is; preset names get the node's flag.
			if prefix := NodePrefix(n.Name); multi && in.DisplayName == "" && prefix != "" {
				base = prefix + " " + base
			}
			// mihomo refuses a profile with two proxies of the same name.
			name := base
			for i := 2; used[name]; i++ {
				name = base + " " + strconv.Itoa(i)
			}
			c, err := proto.ClientConfig(t, proto.ClientInput{Name: name, Host: n.Endpoint.Host, Port: port, PortSpec: in.Port,
				SNI: n.Endpoint.SNI, PinSHA256: n.Endpoint.PinSHA256, Fingerprint: p.Fingerprint, Slot: slot})
			if err != nil {
				p.skip(in, err)
				continue
			}
			used[name] = true
			out = append(out, proxy{name: name, node: n.ID, inbound: in.ID, uri: c.URI, yaml: c.Mihomo})
		}
	}
	return out, nil
}

func (p Profile) skip(in db.Inbound, err error) {
	if p.Skip != nil {
		p.Skip(in, err)
	}
}

func firstPort(spec string) (int, error) {
	head, _, _ := strings.Cut(spec, ",")
	head, _, _ = strings.Cut(head, "-")
	p, err := strconv.Atoi(strings.TrimSpace(head))
	if err != nil || p <= 0 || p > 65535 {
		return 0, fmt.Errorf("bad port %q", spec)
	}
	return p, nil
}

// Link is one inbound's share link, handed out in the admin panel (never on a
// user-facing page): the admin copies it to whoever gets this user.
type Link struct {
	InboundID int64
	Name      string
	URI       string
}

// Links renders one share link per inbound that has one (mihomo-only types
// carry no link and are left out, like in subscriptions).
func Links(p Profile) ([]Link, error) {
	ps, err := build(p)
	if err != nil {
		return nil, err
	}
	var out []Link
	for _, x := range ps {
		if x.uri == "" {
			continue
		}
		out = append(out, Link{InboundID: x.inbound, Name: x.name, URI: x.uri})
	}
	if len(out) == 0 {
		return nil, ErrNoProxies
	}
	return out, nil
}

// URIs renders one share link per inbound.
func URIs(p Profile) (string, error) {
	ps, err := build(p)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(ps))
	for _, x := range ps {
		if x.uri != "" { // the mihomo-only types have no share link
			lines = append(lines, x.uri)
		}
	}
	if len(lines) == 0 {
		return "", ErrNoProxies
	}
	return strings.Join(lines, "\n"), nil
}

// Mihomo renders a complete client profile. mihomo's parser accepts JSON as YAML.
func Mihomo(p Profile, g Groups, r Routing) ([]byte, error) {
	ps, err := build(p)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		// Groups with no proxies in them are a profile mihomo may refuse whole.
		return nil, ErrNoProxies
	}
	g = g.WithDefaults("")
	proxies := make([]map[string]any, len(ps))
	names := make([]string, len(ps))
	for i, x := range ps {
		proxies[i], names[i] = x.yaml, x.name
	}
	countries := countryGroups(p.Nodes, ps, g, names)
	selector := []string{g.Auto}
	for _, c := range countries {
		selector = append(selector, c["name"].(string))
	}
	groups := []map[string]any{
		{"name": g.Main, "type": "select", "proxies": append(selector, names...)},
		urlTest(g.Auto, names),
	}
	groups = append(groups, countries...)
	if g.Main != AliasGroup {
		groups = append(groups, map[string]any{"name": AliasGroup, "type": "select", "proxies": []string{g.Main}, "hidden": true})
	}
	dns := map[string]any{
		"enable": true, "ipv6": false, "enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16",
		// Yandex first: from Russia it answers directly while foreign plain DNS is
		// poisoned or throttled; elsewhere it is a correct anycast recursive.
		"default-nameserver": []string{"77.88.8.8", "1.1.1.1", "8.8.8.8"},
		// Local names must resolve for real: a fake IP for a LAN host or
		// localhost breaks it while the VPN is on.
		"fake-ip-filter": []string{"+.lan", "+.local", "+.home", "+.internal", "+.intranet", "+.localdomain", "localhost"},
		"nameserver":     []string{"https://1.1.1.1/dns-query", "https://dns.google/dns-query"},
	}
	// The panel and the nodes stay out of the tunnel whatever the admin's rules say.
	rules := append(directRules(p.Direct), "GEOIP,LAN,DIRECT,no-resolve")
	rules = append(rules, p.Rules...)
	// AdBlock after the admin's rules: explicit routes win over the automatic
	// block, and blocked ads never leave through the tunnel.
	rules = append(rules, nodeapi.AdBlockRules(p.AdBlock)...)
	cfg := map[string]any{
		"mixed-port": 7890, "allow-lan": false, "mode": "rule", "log-level": "warning",
		// The node has no IPv6 on most VPS: with it on, apps first try IPv6 through the
		// tunnel and wait for the node's "network unreachable" before falling back.
		"ipv6": false, "unified-delay": true, "tcp-concurrent": true,
		"dns":          dns,
		"proxies":      proxies,
		"proxy-groups": groups,
	}
	if r == RoutingRUDirect {
		rules = append(rules, "GEOSITE,category-ru,DIRECT", "GEOIP,ru,DIRECT")
		cfg["geodata-mode"], cfg["geox-url"] = false, geoxURL
		applyProfileDNS(dns, p.DNS, true)
	} else {
		applyProfileDNS(dns, p.DNS, false)
		if needsProfileGeo(rules, p.DNS) {
			cfg["geodata-mode"], cfg["geox-url"] = false, geoxURL
		}
	}
	cfg["rules"] = append(rules, "MATCH,"+g.Main)
	return json.MarshalIndent(cfg, "", "  ")
}

// profileBootstrapDNS is the direct plain DNS the app resolves proxy server
// names (the node it connects to) with. It must stay direct and plain: direct
// DoH stalls under TSPU throttling, and resolving the node through the tunnel
// it has not established yet deadlocks the connection.
var profileBootstrapDNS = []string{"77.88.8.8", "1.1.1.1"}

// profileViaTunnel says whether a default resolver must answer through the
// tunnel in ru_direct mode. Encrypted transports stall directly under TSPU
// throttling, and plain UDP to foreign resolvers is poisoned (a wrong IP for a
// domain whose traffic still enters the tunnel breaks the site). Yandex stays
// direct (Russian answers need no VPN), and local names and addresses must
// never enter the tunnel.
func profileViaTunnel(s string) bool {
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "tls://") || strings.HasPrefix(s, "quic://") {
		return true
	}
	host := s
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		host = u.Hostname() // udp://, tcp://, dhcp://...
	} else if h, _, err := net.SplitHostPort(s); err == nil {
		host = h
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
			return false
		}
		return host != "77.88.8.8" && host != "77.88.8.1"
	}
	return false // a bare name is a local resolver
}

// applyProfileDNS puts the admin's subscription DNS into the profile's dns section.
// Nil doc keeps the built-in one: through the tunnel in both modes (GEOIP,ru makes
// the app resolve every domain itself, direct DoH stalls under TSPU throttling, and
// direct DNS leaks browsing while the traffic goes through the VPN). Russian domains
// resolve with Yandex DNS directly and keep working without the VPN. The alias group
// has a fixed name: "&" or "=" in a renamed group would break the "#group" suffix.
func applyProfileDNS(dns map[string]any, doc *netcfg.SubDNS, ruDirect bool) {
	if doc == nil {
		ns := []string{"https://1.1.1.1/dns-query", "https://dns.google/dns-query"}
		for i, s := range ns {
			ns[i] = s + "#" + AliasGroup
		}
		dns["nameserver"] = ns
		if len(ns) == 0 {
			// mihomo seeds foreign DoH as the default resolvers when the list is
			// absent: an explicit empty list keeps policy-only DNS on its policy
			// and fallback instead of leaking queries.
			dns["nameserver"] = []string{}
		}
		dns["proxy-server-nameserver"] = append([]string{}, profileBootstrapDNS...)
		if ruDirect {
			dns["nameserver-policy"] = map[string]any{"geosite:category-ru": []string{"77.88.8.8", "77.88.8.1"}}
		}
		return
	}
	dns["ipv6"] = doc.IPv6
	if !doc.FakeIP {
		delete(dns, "enhanced-mode")
		delete(dns, "fake-ip-range")
	}
	defaults, policy := nodeapi.SplitServers(doc.Servers)
	ns := defaults
	if ruDirect {
		for i, s := range ns {
			if !strings.Contains(s, "#") && profileViaTunnel(s) {
				ns[i] = s + "#" + AliasGroup
			}
		}
	}
	dns["nameserver"] = ns
	if len(doc.Proxy) > 0 {
		dns["proxy-server-nameserver"] = append([]string{}, doc.Proxy...)
	} else if tunneled := func() bool {
		for _, s := range ns {
			if strings.Contains(s, "#") {
				return true
			}
		}
		return false
	}(); ruDirect && tunneled {
		// The node is reached by name: with every resolver going through the
		// tunnel and none direct, resolving it deadlocks. Keep the bootstrap.
		dns["proxy-server-nameserver"] = append([]string{}, profileBootstrapDNS...)
	} else {
		delete(dns, "proxy-server-nameserver")
	}
	if len(doc.Fallback) > 0 {
		dns["fallback"] = append([]string{}, doc.Fallback...)
		dns["fallback-filter"] = nodeapi.FallbackFilterSection(doc.FallbackFilter)
	} else {
		delete(dns, "fallback")
		delete(dns, "fallback-filter")
	}
	if len(policy) > 0 {
		out := map[string]any{}
		for k, vs := range policy {
			out[k] = append([]string{}, vs...)
		}
		if ruDirect {
			if _, ok := out["geosite:category-ru"]; !ok {
				out["geosite:category-ru"] = []string{"77.88.8.8", "77.88.8.1"}
			}
		}
		dns["nameserver-policy"] = out
	} else if ruDirect {
		dns["nameserver-policy"] = map[string]any{"geosite:category-ru": []string{"77.88.8.8", "77.88.8.1"}}
	} else {
		delete(dns, "nameserver-policy")
	}
}

// needsProfileGeo says whether the profile's rules or DNS policy reference GEO data,
// which the app must download before the profile starts. GEOIP,LAN is built in and
// needs no files.
func needsProfileGeo(rules []string, doc *netcfg.SubDNS) bool {
	for _, r := range rules {
		up := strings.ToUpper(strings.TrimSpace(r))
		if strings.HasPrefix(up, "GEOIP,LAN,") || strings.HasPrefix(up, "GEOIP,LAN ") || up == "GEOIP,LAN" {
			continue
		}
		for _, p := range []string{"GEOIP,", "GEOSITE,", "SRC-GEOIP,"} {
			if strings.HasPrefix(up, p) {
				return true
			}
		}
	}
	if doc != nil {
		for _, s := range doc.Servers {
			for _, k := range s.Domains {
				if strings.HasPrefix(strings.ToLower(k), "geosite:") {
					return true
				}
			}
		}
		if len(doc.FallbackFilter.Geosite) > 0 || len(doc.FallbackFilter.GeoIP) > 0 {
			return true
		}
	}
	return false
}

func urlTest(name string, proxies []string) map[string]any {
	return map[string]any{"name": name, "type": "url-test", "proxies": proxies, "url": "https://www.gstatic.com/generate_204", "interval": 300, "tolerance": 50}
}

// countryGroups picks the fastest proxy of each node when there are several nodes. A
// node whose name would clash with another group or a proxy gets no group: the profile
// must load whatever the admin typed.
func countryGroups(nodes []Node, ps []proxy, g Groups, names []string) []map[string]any {
	if len(nodes) < 2 {
		return nil
	}
	taken := map[string]bool{strings.ToLower(g.Main): true, strings.ToLower(g.Auto): true, strings.ToLower(AliasGroup): true}
	for _, n := range names {
		taken[strings.ToLower(n)] = true
	}
	var out []map[string]any
	for _, n := range nodes {
		var own []string
		for _, x := range ps {
			if x.node == n.ID {
				own = append(own, x.name)
			}
		}
		key := strings.ToLower(n.Name)
		if len(own) == 0 || ValidName(n.Name) != nil || taken[key] {
			continue
		}
		taken[key] = true
		out = append(out, urlTest(n.Name, own))
	}
	return out
}

// directRules keep traffic to the server itself (panel, SSH, subscription updates) out
// of the tunnel: in TUN mode it would otherwise loop through the node and die with it.
func directRules(hosts []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, h := range hosts {
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		switch ip, err := netip.ParseAddr(h); {
		case err != nil:
			out = append(out, "DOMAIN,"+h+",DIRECT")
		case ip.Is4():
			out = append(out, "IP-CIDR,"+ip.String()+"/32,DIRECT,no-resolve")
		default:
			out = append(out, "IP-CIDR6,"+ip.String()+"/128,DIRECT,no-resolve")
		}
	}
	return out
}
