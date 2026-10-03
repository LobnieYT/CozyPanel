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
	MaxDNSServers  = 8
	MaxDNSPolicy   = 64
	MaxDNSHosts    = 512
	MaxRoutes      = 500
	MaxOutbounds   = 32
	MaxSubServers  = 4
	MaxSubPolicy   = 32
	maxRouteLine   = 512
	maxOutboundDoc = 8192
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

// ParseNodeDNS parses a node_dns document; empty means DNS stays off.
func ParseNodeDNS(raw string) (*nodeapi.NodeDNS, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var d nodeapi.NodeDNS
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, &fieldErr{Field: "dns", Code: "bad_json"}
	}
	if !d.Enable {
		return &nodeapi.NodeDNS{}, nil
	}
	for _, s := range d.Nameservers {
		if err := checkServer(s); err != nil {
			return nil, &fieldErr{Field: "nameservers", Code: "bad_server"}
		}
	}
	for _, s := range d.ProxyServers {
		if err := checkServer(s); err != nil {
			return nil, &fieldErr{Field: "proxy_servers", Code: "bad_server"}
		}
	}
	if len(d.Nameservers) == 0 {
		return nil, &fieldErr{Field: "nameservers", Code: "no_servers"}
	}
	if len(d.Nameservers) > MaxDNSServers || len(d.ProxyServers) > MaxDNSServers {
		return nil, &fieldErr{Field: "nameservers", Code: "too_many_servers"}
	}
	if len(d.Policy) > MaxDNSPolicy {
		return nil, &fieldErr{Field: "policy", Code: "too_many_rules"}
	}
	for k, vs := range d.Policy {
		if err := checkPolicyKey(k); err != nil {
			return nil, &fieldErr{Field: "policy", Code: "bad_key"}
		}
		for _, s := range vs {
			if err := checkServer(s); err != nil {
				return nil, &fieldErr{Field: "policy", Code: "bad_server"}
			}
		}
	}
	if len(d.Hosts) > MaxDNSHosts {
		return nil, &fieldErr{Field: "hosts", Code: "too_many_hosts"}
	}
	for k, vs := range d.Hosts {
		if !hostname.Valid(k) {
			return nil, &fieldErr{Field: "hosts", Code: "bad_name"}
		}
		for _, s := range vs {
			if net.ParseIP(s) == nil {
				return nil, &fieldErr{Field: "hosts", Code: "bad_address"}
			}
		}
	}
	return &d, nil
}

// ParseNodeRoutes parses a node_routes document: one mihomo rule per line,
// "TYPE,VALUE,TARGET" with an optional no-resolve, "#" starts a comment (and
// "# default: NAME" sets the MATCH target). The type is stored uppercase,
// as mihomo parses it.
func ParseNodeRoutes(raw string) (nodeapi.NodeRoutes, error) {
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
		rule := typ + "," + value + "," + target
		if flag {
			rule += ",no-resolve"
		}
		out.Rules = append(out.Rules, nodeapi.NodeRoute{Rule: rule})
		if len(out.Rules) > MaxRoutes {
			return out, &fieldErr{Field: "routes", Code: "too_many_rules"}
		}
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
	Nameservers []string            `json:"nameservers,omitempty"`
	Proxy       []string            `json:"proxy_servers,omitempty"`
	Policy      map[string][]string `json:"policy,omitempty"`
	FakeIP      bool                `json:"fake_ip"`
	IPv6        bool                `json:"ipv6,omitempty"`
}

// ParseSubDNS parses a sub_dns document; empty means the built-in profile DNS.
func ParseSubDNS(raw string) (*SubDNS, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var d SubDNS
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, &fieldErr{Field: "dns", Code: "bad_json"}
	}
	if len(d.Nameservers) == 0 {
		return nil, &fieldErr{Field: "nameservers", Code: "no_servers"}
	}
	if len(d.Nameservers) > MaxSubServers || len(d.Proxy) > MaxSubServers {
		return nil, &fieldErr{Field: "nameservers", Code: "too_many_servers"}
	}
	for _, s := range append(append([]string{}, d.Nameservers...), d.Proxy...) {
		if err := checkServer(s); err != nil {
			return nil, &fieldErr{Field: "nameservers", Code: "bad_server"}
		}
	}
	if len(d.Policy) > MaxSubPolicy {
		return nil, &fieldErr{Field: "policy", Code: "too_many_rules"}
	}
	for k, vs := range d.Policy {
		if err := checkPolicyKey(k); err != nil {
			return nil, &fieldErr{Field: "policy", Code: "bad_key"}
		}
		for _, s := range vs {
			if net.ParseIP(s) == nil {
				return nil, &fieldErr{Field: "policy", Code: "bad_server"}
			}
		}
	}
	return &d, nil
}

// checkServer accepts a plain IP or an encrypted DNS endpoint.
func checkServer(s string) error {
	if net.ParseIP(s) != nil {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return errors.New("bad server")
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "tls", "h2c", "https+local", "h2c+local", "quic+local":
		host := u.Hostname()
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

// Effective picks the node's override when set, else the global document.
func Effective(overrideOK bool, override, global string) string {
	if overrideOK && strings.TrimSpace(override) != "" {
		return override
	}
	return global
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
