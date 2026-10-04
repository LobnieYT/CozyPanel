// Package netcfg parses and validates the network documents: per-node DNS, routes
// and outbounds (the global settings or a node's overrides) and subscription DNS.
// The panel validates on save; nodesync parses again when it builds a node's state.
package netcfg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"cozy/internal/hostname"
	"cozy/internal/nodeapi"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/store/db"
)

const (
	MaxDNSServers  = 16
	MaxDNSPolicy   = 64
	MaxDNSHosts    = 512
	MaxRoutes      = 500
	MaxOutbounds   = 32
	MaxSubServers  = 8
	MaxSubPolicy   = 32
	maxRouteLine   = 512
	maxOutboundDoc = 8192
	maxServerTag   = 32
	maxFilterItems = 128
)

// fieldErr is a validation failure naming its field, like the API reports it.
// Line points at the offending line for multi-line documents, 0 otherwise.
type fieldErr struct {
	Field string
	Code  string
	Line  int
}

func (e *fieldErr) Error() string { return e.Field + ": " + e.Code }

// Detail splits err for an API error detail: the field, the stable code and the
// line (0 when none). Unknown errors come back as-is under "invalid".
func Detail(err error) (field, code string, line int) {
	var fe *fieldErr
	if errors.As(err, &fe) {
		return fe.Field, fe.Code, fe.Line
	}
	return "", "invalid", 0
}

// ParseNodeDNS parses a node_dns document; empty means DNS stays off. The flat
// legacy shape (nameservers[] + policy{}) converts to named servers on the fly.
func ParseNodeDNS(raw string) (*nodeapi.NodeDNS, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var keys map[string]any
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil, &fieldErr{Field: "dns", Code: "bad_json"}
	}
	var d nodeapi.NodeDNS
	if _, isNew := keys["servers"]; isNew || !hasLegacyDNSKeys(keys) {
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			return nil, &fieldErr{Field: "dns", Code: "bad_json"}
		}
	} else {
		var old nodeDNSLegacy
		if err := json.Unmarshal([]byte(raw), &old); err != nil {
			return nil, &fieldErr{Field: "dns", Code: "bad_json"}
		}
		d = old.convert()
	}
	if !d.Enable {
		return &nodeapi.NodeDNS{}, nil
	}
	if err := checkNodeDNS(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

// nodeDNSLegacy is the flat pre-servers shape, converted on parse.
type nodeDNSLegacy struct {
	Enable       bool                `json:"enable"`
	IPv6         bool                `json:"ipv6"`
	Nameservers  []string            `json:"nameservers"`
	ProxyServers []string            `json:"proxy_servers"`
	Policy       map[string][]string `json:"policy"`
	Hosts        map[string][]string `json:"hosts"`
}

// hasLegacyDNSKeys says the document uses the flat shape.
func hasLegacyDNSKeys(keys map[string]any) bool {
	for _, k := range []string{"nameservers", "policy"} {
		if _, ok := keys[k]; ok {
			return true
		}
	}
	return false
}

// convert turns flat servers + policy into named servers: policy keys attach to
// the servers named by their values, unknown values become servers of their own.
func (old nodeDNSLegacy) convert() nodeapi.NodeDNS {
	d := nodeapi.NodeDNS{Enable: old.Enable, IPv6: old.IPv6, ProxyServers: old.ProxyServers, Hosts: old.Hosts}
	at := map[string]int{}
	for _, s := range old.Nameservers {
		at[s] = len(d.Servers)
		d.Servers = append(d.Servers, nodeapi.DNSServer{Address: s})
	}
	for key, vs := range old.Policy {
		for _, s := range vs {
			if i, ok := at[s]; ok {
				d.Servers[i].Domains = append(d.Servers[i].Domains, key)
				continue
			}
			at[s] = len(d.Servers)
			d.Servers = append(d.Servers, nodeapi.DNSServer{Address: s, Domains: []string{key}})
		}
	}
	return d
}

// checkNodeDNS validates an enabled node DNS document.
func checkNodeDNS(d *nodeapi.NodeDNS) error {
	if len(d.Servers) == 0 {
		return &fieldErr{Field: "servers", Code: "no_servers"}
	}
	if len(d.Servers) > MaxDNSServers {
		return &fieldErr{Field: "servers", Code: "too_many_servers"}
	}
	def := false
	for i := range d.Servers {
		if err := checkDNSServer(&d.Servers[i]); err != nil {
			return err
		}
		if len(d.Servers[i].Domains) == 0 {
			def = true
		}
	}
	// mihomo refuses an enabled DNS with an empty nameserver even when a
	// fallback exists: what nothing else claims must go to a default server.
	if !def {
		return &fieldErr{Field: "servers", Code: "no_default"}
	}
	for _, s := range d.ProxyServers {
		if err := checkServer(s); err != nil {
			return &fieldErr{Field: "proxy_servers", Code: "bad_server"}
		}
	}
	if len(d.ProxyServers) > MaxDNSServers {
		return &fieldErr{Field: "proxy_servers", Code: "too_many_servers"}
	}
	for _, s := range d.Fallback {
		if err := checkServer(s); err != nil {
			return &fieldErr{Field: "fallback", Code: "bad_server"}
		}
	}
	if err := checkFallbackFilter(&d.FallbackFilter); err != nil {
		return err
	}
	if len(d.Hosts) > MaxDNSHosts {
		return &fieldErr{Field: "hosts", Code: "too_many_hosts"}
	}
	for k, vs := range d.Hosts {
		if !hostname.Valid(k) {
			return &fieldErr{Field: "hosts", Code: "bad_name"}
		}
		for _, s := range vs {
			if net.ParseIP(s) == nil {
				return &fieldErr{Field: "hosts", Code: "bad_address"}
			}
		}
	}
	return nil
}

// checkDNSServer validates one named resolver.
func checkDNSServer(s *nodeapi.DNSServer) error {
	if err := checkServer(s.Address); err != nil {
		return &fieldErr{Field: "servers", Code: "bad_server"}
	}
	if s.Port < 0 || s.Port > 65535 {
		return &fieldErr{Field: "servers", Code: "bad_port"}
	}
	if s.Tag != "" && (len(s.Tag) > maxServerTag || strings.ContainsAny(s.Tag, ",\n\r")) {
		return &fieldErr{Field: "servers", Code: "bad_tag"}
	}
	if len(s.Domains) > MaxDNSPolicy {
		return &fieldErr{Field: "servers", Code: "too_many_rules"}
	}
	for _, k := range s.Domains {
		if err := checkPolicyKey(k); err != nil {
			return &fieldErr{Field: "servers", Code: "bad_key"}
		}
	}
	return nil
}

// checkFallbackFilter validates poisoning-protection lists. mihomo filters a
// single GEOIP country code, so at most one travels (the node renders it as
// geoip-code); more would silently not apply.
func checkFallbackFilter(f *nodeapi.DNSFallbackFilter) error {
	count := 0
	for _, v := range f.GeoIP {
		if !validGeoName(v) {
			return &fieldErr{Field: "fallback_filter", Code: "bad_key"}
		}
		count++
	}
	if len(f.GeoIP) > 1 {
		return &fieldErr{Field: "fallback_filter", Code: "too_many_rules"}
	}
	for _, v := range f.Geosite {
		if !validGeoName(v) {
			return &fieldErr{Field: "fallback_filter", Code: "bad_key"}
		}
		count++
	}
	for _, v := range f.IPCIDR {
		if _, err := netip.ParsePrefix(v); err != nil {
			if net.ParseIP(v) == nil {
				return &fieldErr{Field: "fallback_filter", Code: "bad_key"}
			}
		}
		count++
	}
	for _, v := range f.Domain {
		if !hostname.Valid(strings.TrimPrefix(v, "+.")) {
			return &fieldErr{Field: "fallback_filter", Code: "bad_key"}
		}
		count++
	}
	if count > maxFilterItems {
		return &fieldErr{Field: "fallback_filter", Code: "too_many_rules"}
	}
	return nil
}

// validGeoName accepts geoip/geosite category names.
func validGeoName(v string) bool {
	if v == "" || len(v) > 64 {
		return false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// ParseNodeRoutes parses a node_routes document: one mihomo rule per line,
// "TYPE,VALUE,TARGET" with an optional no-resolve, "#" starts a comment (and
// "# default: NAME" sets the MATCH target). The type is stored uppercase,
// as mihomo parses it.
func ParseNodeRoutes(raw string) (nodeapi.NodeRoutes, error) {
	return ParseNodeRoutesChecked(raw, nil)
}

// RouteTargetKnown allows rule targets naming the builtins, the WARP and
// NODE-<id> exits by shape, or one of the outbounds. The panel checks without
// the node's state, so WARP and exits pass by shape; the node re-checks
// exactly. A nil known skips the target check.
func RouteTargetKnown(outbounds []nodeapi.NodeOutbound) func(string) bool {
	allow := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "WARP": true}
	for _, o := range outbounds {
		allow[o.Name] = true
	}
	return func(name string) bool {
		return allow[name] || strings.HasPrefix(name, "NODE-")
	}
}

// ParseNodeRoutesChecked parses like ParseNodeRoutes and additionally refuses
// targets known does not allow, with the offending line. mihomo resolves
// target names lazily, so a typo would pass its parser and kill the matching
// traffic at runtime instead of failing the save.
func ParseNodeRoutesChecked(raw string, known func(string) bool) (nodeapi.NodeRoutes, error) {
	out := nodeapi.NodeRoutes{}
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if rest, ok := strings.CutPrefix(line, "# default:"); ok {
				name := strings.TrimSpace(rest)
				if name == "" || strings.ContainsAny(name, " \t,") {
					return out, &fieldErr{"routes", "bad_default", i + 1}
				}
				out.Default = name
			}
			continue
		}
		if len(line) > maxRouteLine {
			return out, &fieldErr{"routes", "line_too_long", i + 1}
		}
		typ, value, target, flag, err := splitRule(line)
		if err != nil {
			return out, &fieldErr{"routes", "bad_rule", i + 1}
		}
		if err := checkRuleValue(typ, value); err != nil {
			return out, &fieldErr{"routes", "bad_value", i + 1}
		}
		if target == "" || strings.ContainsAny(target, " \t,") {
			return out, &fieldErr{"routes", "bad_target", i + 1}
		}
		if known != nil && !known(target) {
			return out, &fieldErr{"routes", "bad_target", i + 1}
		}
		rule := typ + "," + value + "," + target
		if flag {
			rule += ",no-resolve"
		}
		out.Rules = append(out.Rules, nodeapi.NodeRoute{Rule: rule})
		if len(out.Rules) > MaxRoutes {
			return out, &fieldErr{Field: "routes", Code: "too_many_rules"}
		}
	}
	if known != nil && out.Default != "" && !known(out.Default) {
		return out, &fieldErr{Field: "routes", Code: "bad_default"}
	}
	return out, nil
}

// ParseNodeOutbounds parses a node_outbounds document: a JSON array of mihomo
// proxies, each with "name" and "type". Reserved names (DIRECT, REJECT and the
// panel's own WARP and NODE-<id> proxies) are refused.
func ParseNodeOutbounds(raw string) ([]nodeapi.NodeOutbound, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > MaxOutbounds*maxOutboundDoc {
		return nil, &fieldErr{Field: "outbounds", Code: "too_big"}
	}
	var docs []map[string]any
	if err := json.Unmarshal([]byte(raw), &docs); err != nil {
		return nil, &fieldErr{Field: "outbounds", Code: "bad_json"}
	}
	if len(docs) > MaxOutbounds {
		return nil, &fieldErr{Field: "outbounds", Code: "too_many_outbounds"}
	}
	out := make([]nodeapi.NodeOutbound, 0, len(docs))
	seen := map[string]bool{}
	for _, doc := range docs {
		name, _ := doc["name"].(string)
		typ, _ := doc["type"].(string)
		if name == "" || typ == "" {
			return nil, &fieldErr{Field: "outbounds", Code: "no_name_or_type"}
		}
		if strings.ContainsAny(name, " \t,") || len(name) > 64 {
			return nil, &fieldErr{Field: "outbounds", Code: "bad_name"}
		}
		up := strings.ToUpper(name)
		if up == "DIRECT" || up == "REJECT" || up == "REJECT-DROP" || up == "WARP" || strings.HasPrefix(up, "NODE-") {
			return nil, &fieldErr{Field: "outbounds", Code: "reserved_name"}
		}
		if seen[up] {
			return nil, &fieldErr{Field: "outbounds", Code: "duplicate_name"}
		}
		seen[up] = true
		raw, err := json.Marshal(doc)
		if err != nil {
			return nil, &fieldErr{Field: "outbounds", Code: "bad_json"}
		}
		out = append(out, nodeapi.NodeOutbound{Name: name, Config: raw})
	}
	return out, nil
}

// WireGuardConf parses a WireGuard .conf ([Interface] + [Peer]) into a mihomo
// wireguard proxy map: server/port from the peer endpoint, addresses from the
// interface. This is how provider configs (NordVPN and the like) become outbounds.
func WireGuardConf(name, conf string) (nodeapi.NodeOutbound, error) {
	var out nodeapi.NodeOutbound
	section := ""
	iface := map[string]string{}
	peer := map[string]string{}
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch section {
		case "interface":
			iface[k] = v
		case "peer":
			peer[k] = v
		}
	}
	private := iface["privatekey"]
	public := peer["publickey"]
	endpoint := peer["endpoint"]
	if private == "" || public == "" || endpoint == "" {
		return out, &fieldErr{Field: "conf", Code: "bad_conf"}
	}
	host, port, ok := strings.Cut(endpoint, ":")
	if !ok || host == "" {
		return out, &fieldErr{Field: "conf", Code: "bad_endpoint"}
	}
	if _, err := strconv.Atoi(port); err != nil {
		return out, &fieldErr{Field: "conf", Code: "bad_endpoint"}
	}
	ips := []string{}
	for _, a := range strings.Split(iface["address"], ",") {
		if a = strings.TrimSpace(a); a != "" {
			addr, _, err := net.ParseCIDR(a)
			if err != nil {
				if net.ParseIP(a) == nil {
					return out, &fieldErr{Field: "conf", Code: "bad_address"}
				}
				addr = net.ParseIP(a)
			}
			ips = append(ips, addr.String())
		}
	}
	allowed := []string{}
	for _, a := range strings.Split(peer["allowedips"], ",") {
		if a = strings.TrimSpace(a); a != "" {
			allowed = append(allowed, a)
		}
	}
	if len(allowed) == 0 {
		allowed = []string{"0.0.0.0/0", "::/0"}
	}
	doc := map[string]any{
		"name": name, "type": "wireguard",
		"server": host, "port": port,
		"ip": strings.Join(ips, ","), "ipv6": "",
		"private-key": private,
		"peers": []any{map[string]any{
			"server": host, "port": port, "public-key": public, "allowed-ips": allowed,
		}},
	}
	if mtu, err := strconv.Atoi(iface["mtu"]); err == nil && mtu > 0 {
		doc["mtu"] = mtu
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return out, &fieldErr{Field: "conf", Code: "bad_conf"}
	}
	return nodeapi.NodeOutbound{Name: name, Config: raw}, nil
}

// SubDNS is subscription DNS: what the panel puts into client profiles.
type SubDNS struct {
	Servers        []nodeapi.DNSServer       `json:"servers,omitempty"`
	Proxy          []string                  `json:"proxy_servers,omitempty"`
	Fallback       []string                  `json:"fallback,omitempty"`
	FallbackFilter nodeapi.DNSFallbackFilter `json:"fallback_filter,omitempty"`
	FakeIP         bool                      `json:"fake_ip"`
	IPv6           bool                      `json:"ipv6,omitempty"`
}

// ParseSubDNS parses a sub_dns document; empty means the built-in profile DNS.
// The legacy flat shape converts like node DNS.
func ParseSubDNS(raw string) (*SubDNS, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var keys map[string]any
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil, &fieldErr{Field: "dns", Code: "bad_json"}
	}
	var d SubDNS
	if _, isNew := keys["servers"]; isNew || !hasSubLegacyKeys(keys) {
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			return nil, &fieldErr{Field: "dns", Code: "bad_json"}
		}
	} else {
		var old subDNSLegacy
		if err := json.Unmarshal([]byte(raw), &old); err != nil {
			return nil, &fieldErr{Field: "dns", Code: "bad_json"}
		}
		d = old.convert()
	}
	if len(d.Servers) == 0 {
		return nil, &fieldErr{Field: "servers", Code: "no_servers"}
	}
	if len(d.Servers) > MaxSubServers {
		return nil, &fieldErr{Field: "servers", Code: "too_many_servers"}
	}
	def := false
	for i := range d.Servers {
		if err := checkDNSServer(&d.Servers[i]); err != nil {
			return nil, err
		}
		if len(d.Servers[i].Domains) == 0 {
			def = true
		}
	}
	// mihomo refuses an enabled DNS with an empty nameserver even when a
	// fallback exists: what nothing else claims must go to a default server.
	if !def {
		return nil, &fieldErr{Field: "servers", Code: "no_default"}
	}
	for _, s := range append(append([]string{}, d.Proxy...), d.Fallback...) {
		if err := checkServer(s); err != nil {
			return nil, &fieldErr{Field: "servers", Code: "bad_server"}
		}
	}
	if err := checkFallbackFilter(&d.FallbackFilter); err != nil {
		return nil, err
	}
	return &d, nil
}

// subDNSLegacy is the flat pre-servers shape, converted on parse.
type subDNSLegacy struct {
	Nameservers []string            `json:"nameservers"`
	Proxy       []string            `json:"proxy_servers"`
	Policy      map[string][]string `json:"policy"`
	FakeIP      bool                `json:"fake_ip"`
	IPv6        bool                `json:"ipv6"`
}

func hasSubLegacyKeys(keys map[string]any) bool {
	for _, k := range []string{"nameservers", "policy"} {
		if _, ok := keys[k]; ok {
			return true
		}
	}
	return false
}

func (old subDNSLegacy) convert() SubDNS {
	d := SubDNS{Proxy: old.Proxy, FakeIP: old.FakeIP, IPv6: old.IPv6}
	at := map[string]int{}
	for _, s := range old.Nameservers {
		at[s] = len(d.Servers)
		d.Servers = append(d.Servers, nodeapi.DNSServer{Address: s})
	}
	for key, vs := range old.Policy {
		for _, s := range vs {
			if i, ok := at[s]; ok {
				d.Servers[i].Domains = append(d.Servers[i].Domains, key)
				continue
			}
			at[s] = len(d.Servers)
			d.Servers = append(d.Servers, nodeapi.DNSServer{Address: s, Domains: []string{key}})
		}
	}
	return d
}

// checkServer accepts a plain IP or a DNS endpoint mihomo parses: udp/tcp,
// tls, http(s), quic, dhcp or system.
func checkServer(s string) error {
	if net.ParseIP(s) != nil {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return errors.New("bad server")
	}
	switch strings.ToLower(u.Scheme) {
	case "udp", "tcp", "tls", "https", "http", "quic", "dhcp", "system":
		host := u.Hostname()
		if u.Scheme == "system" || u.Scheme == "dhcp" {
			return nil
		}
		if host == "" || (!hostname.Valid(host) && net.ParseIP(host) == nil) {
			return errors.New("bad host")
		}
		return nil
	}
	return errors.New("bad scheme")
}

// checkPolicyKey accepts "geosite:<name>", "domain:<suffix>" or an IP network.
func checkPolicyKey(k string) error {
	if rest, ok := strings.CutPrefix(k, "geosite:"); ok {
		if rest == "" || len(rest) > 64 {
			return errors.New("bad geosite")
		}
		for _, r := range rest {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return errors.New("bad geosite")
			}
		}
		return nil
	}
	if rest, ok := strings.CutPrefix(k, "domain:"); ok {
		if !hostname.Valid(strings.TrimPrefix(rest, ".")) {
			return errors.New("bad domain")
		}
		return nil
	}
	if _, err := netip.ParsePrefix(k); err == nil {
		return nil
	}
	if net.ParseIP(k) != nil {
		return nil
	}
	return errors.New("bad key")
}

// splitRule splits "TYPE,VALUE,TARGET[,no-resolve]".
func splitRule(line string) (typ, value, target string, noResolve bool, err error) {
	parts := strings.Split(line, ",")
	if len(parts) < 3 || len(parts) > 4 {
		return "", "", "", false, errors.New("bad parts")
	}
	typ = strings.ToUpper(strings.TrimSpace(parts[0]))
	value = strings.TrimSpace(parts[1])
	target = strings.TrimSpace(parts[2])
	if len(parts) == 4 {
		if strings.ToLower(strings.TrimSpace(parts[3])) != "no-resolve" {
			return "", "", "", false, errors.New("bad flag")
		}
		noResolve = true
	}
	if typ == "" || value == "" || target == "" {
		return "", "", "", false, errors.New("empty")
	}
	switch typ {
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-REGEX", "DOMAIN-WILDCARD",
		"GEOSITE", "GEOIP", "IP-CIDR", "IP-CIDR6", "IP-SUFFIX", "IP-ASN", "SRC-IP-CIDR",
		"DST-PORT", "SRC-PORT", "NETWORK", "IN-NAME", "PROCESS-NAME", "PROCESS-PATH", "SRC-GEOIP":
	default:
		return "", "", "", false, errors.New("bad type")
	}
	return typ, value, target, noResolve, nil
}

// checkRuleValue checks the VALUE against its TYPE.
func checkRuleValue(typ, value string) error {
	switch typ {
	case "IP-CIDR", "IP-CIDR6", "SRC-IP-CIDR", "IP-SUFFIX":
		if _, err := netip.ParsePrefix(value); err != nil {
			if net.ParseIP(value) == nil {
				return err
			}
		}
		return nil
	case "DST-PORT", "SRC-PORT":
		return checkPorts(value)
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-WILDCARD", "PROCESS-NAME", "PROCESS-PATH":
		if value == "" || len(value) > 253 {
			return errors.New("bad value")
		}
		return nil
	case "DOMAIN-REGEX":
		return checkRegexp(value)
	case "GEOSITE", "GEOIP", "SRC-GEOIP":
		if value == "" || len(value) > 64 {
			return errors.New("bad value")
		}
		return nil
	case "IP-ASN":
		if _, err := strconv.Atoi(strings.TrimPrefix(value, "AS")); err != nil {
			return err
		}
		return nil
	case "NETWORK":
		switch strings.ToUpper(value) {
		case "TCP", "UDP":
			return nil
		}
		return errors.New("bad network")
	}
	return nil
}

// checkPorts accepts "80", "80-443" and "/443".
func checkPorts(value string) error {
	v := strings.TrimPrefix(value, "/")
	lo, hi, _ := strings.Cut(v, "-")
	loN, err := strconv.Atoi(lo)
	if err != nil || loN < 0 || loN > 65535 {
		return errors.New("bad port")
	}
	if hi != "" {
		hiN, err := strconv.Atoi(hi)
		if err != nil || hiN < loN || hiN > 65535 {
			return errors.New("bad port")
		}
	}
	return nil
}

// checkRegexp bounds the pattern; the node parses it against mihomo's syntax.
func checkRegexp(value string) error {
	if len(value) > 256 {
		return errors.New("too long")
	}
	return nil
}

// ParseAdBlock parses a node_adblock/sub_adblock document.
func ParseAdBlock(raw string) (*nodeapi.AdBlock, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var d nodeapi.AdBlock
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, &fieldErr{Field: "adblock", Code: "bad_json"}
	}
	if !d.Enabled {
		return &nodeapi.AdBlock{}, nil
	}
	if len(d.Exceptions) > MaxDNSHosts || len(d.Extra) > MaxDNSHosts {
		return nil, &fieldErr{Field: "adblock", Code: "too_many_hosts"}
	}
	for _, s := range append(append([]string{}, d.Exceptions...), d.Extra...) {
		if !hostname.Valid(strings.TrimPrefix(strings.TrimSpace(s), ".")) || strings.TrimSpace(s) == "" {
			return nil, &fieldErr{Field: "adblock", Code: "bad_name"}
		}
	}
	return &d, nil
}

// Resolved is a node's effective network setup: overrides win over globals.
type Resolved struct {
	DNS       *nodeapi.NodeDNS
	Routes    nodeapi.NodeRoutes
	Outbounds []nodeapi.NodeOutbound
}

// Resolve reads a node's effective documents (override or global setting) and
// parses them. q reads settings, set resolves them.
func Resolve(ctx context.Context, q *db.Queries, set *settings.Settings, n db.Node) (Resolved, error) {
	var out Resolved
	pick := func(ov sql.NullString, key string) (string, error) {
		if ov.Valid && strings.TrimSpace(ov.String) != "" {
			return ov.String, nil
		}
		return set.String(ctx, key)
	}
	dnsRaw, err := pick(n.DnsOverride, settings.KeyNodeDNS)
	if err != nil {
		return out, err
	}
	routesRaw, err := pick(n.RoutesOverride, settings.KeyNodeRoutes)
	if err != nil {
		return out, err
	}
	outRaw, err := pick(n.OutboundsOverride, settings.KeyNodeOutbounds)
	if err != nil {
		return out, err
	}
	if out.DNS, err = ParseNodeDNS(dnsRaw); err != nil {
		return out, err
	}
	if out.Routes, err = ParseNodeRoutes(routesRaw); err != nil {
		return out, err
	}
	if out.Outbounds, err = ParseNodeOutbounds(outRaw); err != nil {
		return out, err
	}
	return out, nil
}

// RouteInput is a synthetic connection to test rules against, without traffic.
type RouteInput struct {
	Domain  string
	IP      string
	Port    int
	Network string // tcp (default) or udp
	Inbound string
}

// MatchRoute returns the first rule matching in, like mihomo would. GEO and
// process rules cannot be matched dryly: they are skipped and geoSkipped says
// a live test on the node is needed for a verdict.
func MatchRoute(rules []nodeapi.NodeRoute, in RouteInput) (rule, target string, matched, geoSkipped bool) {
	for _, r := range rules {
		typ, value, tgt, _, err := splitRule(r.Rule)
		if err != nil {
			continue
		}
		switch typ {
		case "GEOIP", "GEOSITE", "SRC-GEOIP", "IP-ASN":
			geoSkipped = true
			continue
		case "PROCESS-NAME", "PROCESS-PATH":
			continue
		}
		if matchRuleValue(typ, value, in) {
			return r.Rule, tgt, true, geoSkipped
		}
	}
	return "", "", false, geoSkipped
}

// matchRuleValue matches one rule value against the input.
func matchRuleValue(typ, value string, in RouteInput) bool {
	switch typ {
	case "DOMAIN":
		return in.Domain != "" && strings.EqualFold(in.Domain, value)
	case "DOMAIN-SUFFIX":
		v := strings.TrimPrefix(value, ".")
		return in.Domain != "" && (strings.EqualFold(in.Domain, v) || strings.HasSuffix(strings.ToLower(in.Domain), "."+strings.ToLower(v)))
	case "DOMAIN-KEYWORD":
		return in.Domain != "" && strings.Contains(strings.ToLower(in.Domain), strings.ToLower(value))
	case "DOMAIN-REGEX":
		ok, err := regexpMatch(value, in.Domain)
		return err == nil && ok
	case "DOMAIN-WILDCARD":
		return in.Domain != "" && wildcardMatch(value, in.Domain)
	case "IP-CIDR", "IP-CIDR6", "SRC-IP-CIDR":
		addr, err := netip.ParseAddr(in.IP)
		if err != nil {
			return false
		}
		if p, err := netip.ParsePrefix(value); err == nil {
			return p.Contains(addr)
		}
		if ip, err := netip.ParseAddr(value); err == nil {
			return ip == addr
		}
		return false
	case "IP-SUFFIX":
		return in.IP != "" && strings.HasSuffix(in.IP, value)
	case "DST-PORT":
		return matchPort(value, in.Port)
	case "SRC-PORT":
		return false // the test has no source port
	case "NETWORK":
		nw := strings.ToUpper(in.Network)
		if nw == "" {
			nw = "TCP"
		}
		return nw == strings.ToUpper(value)
	case "IN-NAME":
		return in.Inbound != "" && strings.EqualFold(in.Inbound, value)
	}
	return false
}

// wildcardMatch matches "*" as "any run of characters", anchored on both ends.
func wildcardMatch(pattern, s string) bool {
	var b strings.Builder
	b.WriteString("(?i)^")
	for _, r := range pattern {
		if r == '*' {
			b.WriteString(".*")
			continue
		}
		b.WriteString(regexp.QuoteMeta(string(r)))
	}
	b.WriteString("$")
	ok, err := regexp.MatchString(b.String(), s)
	return err == nil && ok
}

// regexpMatch compiles a DOMAIN-REGEX. mihomo uses RE2, like Go: close enough
// for a dry test, the node has the last word.
func regexpMatch(pattern, s string) (bool, error) {
	if s == "" {
		return false, nil
	}
	return regexp.MatchString(pattern, s)
}

// matchPort matches "80", "80-443" and "/443".
func matchPort(value string, port int) bool {
	v := strings.TrimPrefix(value, "/")
	lo, hi, _ := strings.Cut(v, "-")
	loN, err := strconv.Atoi(lo)
	if err != nil {
		return false
	}
	if hi == "" {
		return port == loN
	}
	hiN, err := strconv.Atoi(hi)
	if err != nil {
		return false
	}
	return port >= loN && port <= hiN
}
