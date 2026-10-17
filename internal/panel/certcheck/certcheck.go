// Package certcheck is "Check the certificate": what the panel's and the nodes' ports
// serve as seen from outside, whether the domain leads here, and whether port 80 reaches
// the panel for the CA. Every finding is a code the admin panel translates with what to
// do about it.
package certcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"cozy/internal/panel/acme"
)

// The outcomes of a check: ok, info (worth knowing, nothing broken), warn (works, but not
// everywhere), fail (does not work).
const (
	OK   = "ok"
	Info = "info"
	Warn = "warn"
	Fail = "fail"
)

// The fixes the admin panel offers: a button, or a command to copy.
const (
	FixNone    = ""
	FixSetCert = "set-certificate"
	FixDomain  = "settings-domain"
	FixPort80  = "free-port-80"
	FixRetry   = "renew-certificate"
)

// CertCheck is one finding.
type CertCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

// CertReport is every finding, the worst first.
type CertReport struct {
	Checks    []CertCheck `json:"checks"`
	CheckedAt time.Time   `json:"checked_at"`
}

// Input is what is checked.
type Input struct {
	Domain string // the panel's domain, "" without one
	Host   string // the panel's address: the domain, or the public IP
	Ports  []int  // the panel's port, and the subscription port when it has one
	// Expected is the leaf the panel serves now: what answers instead is someone else's.
	Expected *x509.Certificate
	Status   acme.Status
	// Challenge is the panel's HTTP-01 responder; nil skips the port 80 check.
	Challenge httpResponder
	Nodes     []Node
}

// httpResponder is the HTTP-01 responder: nil when the panel has none.
type httpResponder interface{ Present() bool }

// Node is a node to check.
type Node struct {
	ID    int64
	Name  string
	Local bool
	Host  string // where clients reach it
	SNI   string // its domain, "" on an IP
	Kind  string // custom, acme, panel, self-signed: what it serves on TLS
	// Pinned: links pin a self-signed certificate.
	Pinned   bool
	Expected *x509.Certificate
	Ports    []Port
}

// Port is a listener on TLS over TCP.
type Port struct {
	Inbound string
	Port    int
}

// Checker runs the checks; its fields are what a test replaces.
type Checker struct {
	DNS interface {
		Lookup(ctx context.Context, name string) ([]netip.Addr, error)
	}
	// Dial returns the chain a TLS server at addr presents for sni.
	Dial  func(ctx context.Context, addr, sni string) ([]*x509.Certificate, error)
	Roots *x509.CertPool // nil: the system's
	Now   func() time.Time
}

// New checks with the system's resolver, roots and network.
func New(dns interface {
	Lookup(ctx context.Context, name string) ([]netip.Addr, error)
}) *Checker {
	return &Checker{DNS: dns, Dial: dialChain, Now: time.Now}
}

// Run checks everything in parallel; it takes a few seconds.
func (c *Checker) Run(ctx context.Context, in Input) CertReport {
	var (
		mu  sync.Mutex
		out []CertCheck
		wg  sync.WaitGroup
	)
	add := func(cs ...CertCheck) {
		mu.Lock()
		out = append(out, cs...)
		mu.Unlock()
	}
	ports := dedupe(in.Ports)
	for _, p := range ports {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			add(c.served(ctx, in, p)...)
		}(p)
	}
	wg.Add(1)
	go func() { defer wg.Done(); add(c.domain(ctx, in)...) }()
	wg.Add(1)
	go func() { defer wg.Done(); add(c.port80(ctx, in)...) }()
	add(acmeStatus(in.Status)...)
	for _, n := range in.Nodes {
		add(nodeCert(n)...)
		for _, p := range n.Ports {
			wg.Add(1)
			go func(n Node, p Port) {
				defer wg.Done()
				add(c.nodePort(ctx, n, p)...)
			}(n, p)
		}
	}
	wg.Wait()
	sortChecks(out)
	return CertReport{Checks: out, CheckedAt: c.Now()}
}

func dedupe(ports []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(ports))
	for _, p := range ports {
		if p > 0 && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// rank orders findings: what fails first, what is fine last; then as they came.
func rank(s string) int {
	switch s {
	case Fail:
		return 0
	case Warn:
		return 1
	case Info:
		return 2
	}
	return 3
}

func sortChecks(cs []CertCheck) {
	sort.SliceStable(cs, func(i, j int) bool { return rank(cs[i].Status) < rank(cs[j].Status) })
}

// served is what the panel's port presents to a client that comes by its address.
func (c *Checker) served(ctx context.Context, in Input, port int) []CertCheck {
	out := []CertCheck{}
	sni := in.Domain
	if sni == "" {
		sni = ""
	}
	chain, err := c.Dial(ctx, net.JoinHostPort(in.Host, fmt.Sprint(port)), sni)
	if err != nil {
		out = append(out, CertCheck{ID: "served_" + fmt.Sprint(port), Status: Fail,
			Detail: err.Error(), Fix: FixNone})
		return out
	}
	leaf := chain[0]
	if in.Expected != nil && !leaf.Equal(in.Expected) {
		out = append(out, CertCheck{ID: "served_" + fmt.Sprint(port), Status: Fail,
			Detail: "a different certificate answers than the panel serves", Fix: FixSetCert})
		return out
	}
	if err := leaf.VerifyHostname(sniOrIP(sni, in.Host)); err != nil {
		out = append(out, CertCheck{ID: "served_" + fmt.Sprint(port), Status: Fail,
			Detail: err.Error(), Fix: FixSetCert})
		return out
	}
	roots := c.Roots
	if roots == nil {
		pool, err := x509.SystemCertPool()
		if err == nil {
			roots = pool
		}
	}
	if roots != nil {
		inter := x509.NewCertPool()
		for _, cert := range chain[1:] {
			inter.AddCert(cert)
		}
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter,
			CurrentTime: c.now()}); err != nil {
			out = append(out, CertCheck{ID: "served_" + fmt.Sprint(port), Status: Warn,
				Detail: "the chain does not verify against the system roots: " + err.Error()})
		}
	}
	left := leaf.NotAfter.Sub(c.now())
	switch {
	case left <= 0:
		out = append(out, CertCheck{ID: "served_" + fmt.Sprint(port), Status: Fail,
			Detail: "the certificate has expired", Fix: FixRetry})
	case left < 7*24*time.Hour:
		out = append(out, CertCheck{ID: "served_" + fmt.Sprint(port), Status: Warn,
			Detail: "the certificate expires in " + left.Round(time.Hour).String()})
	}
	if len(out) == 0 {
		out = append(out, CertCheck{ID: "served_" + fmt.Sprint(port), Status: OK})
	}
	return out
}

func sniOrIP(sni, host string) string {
	if sni != "" {
		return sni
	}
	return host
}

func (c *Checker) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// domain: does the name lead to this server.
func (c *Checker) domain(ctx context.Context, in Input) []CertCheck {
	if in.Domain == "" {
		return []CertCheck{{ID: "domain", Status: Info, Detail: "no domain is set: the certificate is for the address"}}
	}
	if c.DNS == nil {
		return nil
	}
	addrs, err := c.DNS.Lookup(ctx, in.Domain)
	if err != nil {
		return []CertCheck{{ID: "domain", Status: Fail, Detail: err.Error(), Fix: FixDomain}}
	}
	for _, a := range addrs {
		if a.String() == in.Host {
			return []CertCheck{{ID: "domain", Status: OK}}
		}
	}
	return []CertCheck{{ID: "domain", Status: Warn,
		Detail: "the domain leads elsewhere: " + strings.Join(addrStrings(addrs), ", "), Fix: FixDomain}}
}

func addrStrings(as []netip.Addr) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.String())
	}
	return out
}

// port80: the CA's challenge needs it free.
func (c *Checker) port80(ctx context.Context, in Input) []CertCheck {
	if in.Challenge == nil || !in.Challenge.Present() {
		return nil
	}
	ln, err := net.Listen("tcp", ":80")
	if err != nil {
		return []CertCheck{{ID: "port80", Status: Fail, Detail: err.Error(), Fix: FixPort80}}
	}
	_ = ln.Close()
	return []CertCheck{{ID: "port80", Status: OK}}
}

// acmeStatus is what the panel's own certificate story is.
func acmeStatus(st acme.Status) []CertCheck {
	switch st.Kind {
	case "letsencrypt":
		return []CertCheck{{ID: "acme", Status: OK}}
	case "custom":
		s := Info
		if !st.Trusted {
			s = Warn
		}
		out := []CertCheck{{ID: "acme", Status: s, Detail: "the panel serves the admin's own certificate"}}
		if st.Error != "" {
			out = append(out, CertCheck{ID: "acme_error", Status: Fail, Detail: st.Error, Fix: FixRetry})
		}
		return out
	}
	out := []CertCheck{{ID: "acme", Status: Warn, Detail: "the panel serves a self-signed certificate: clients warn"}}
	if st.Error != "" {
		out = append(out, CertCheck{ID: "acme_error", Status: Fail, Detail: st.Error, Fix: FixRetry})
	}
	return out
}

// nodeCert is what a node serves for itself.
func nodeCert(n Node) []CertCheck {
	if n.Expected == nil {
		return nil
	}
	sni := n.SNI
	if sni == "" {
		sni = n.Host
	}
	if err := n.Expected.VerifyHostname(sni); err != nil {
		return []CertCheck{{ID: "node_" + fmt.Sprint(n.ID) + "_cert", Status: Fail,
			Detail: n.Name + ": " + err.Error()}}
	}
	return []CertCheck{{ID: "node_" + fmt.Sprint(n.ID) + "_cert", Status: OK}}
}

// nodePort is what a node's listener presents.
func (c *Checker) nodePort(ctx context.Context, n Node, p Port) []CertCheck {
	if n.Expected == nil {
		return nil
	}
	sni := n.SNI
	if sni == "" {
		sni = n.Host
	}
	chain, err := c.Dial(ctx, net.JoinHostPort(n.Host, fmt.Sprint(p.Port)), sni)
	if err != nil {
		return []CertCheck{{ID: "node_" + fmt.Sprint(n.ID) + "_" + p.Inbound, Status: Fail,
			Detail: n.Name + ": " + err.Error()}}
	}
	if !chain[0].Equal(n.Expected) {
		return []CertCheck{{ID: "node_" + fmt.Sprint(n.ID) + "_" + p.Inbound, Status: Warn,
			Detail: n.Name + ": a different certificate answers"}}
	}
	return []CertCheck{{ID: "node_" + fmt.Sprint(n.ID) + "_" + p.Inbound, Status: OK}}
}

// dialChain is the chain a TLS server at addr presents for sni.
func dialChain(ctx context.Context, addr, sni string) ([]*x509.Certificate, error) {
	d := &tls.Dialer{Config: &tls.Config{ServerName: sni, InsecureSkipVerify: true}} //nolint:gosec // the chain is what is checked
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	tc, ok := conn.(*tls.Conn)
	if !ok {
		return nil, fmt.Errorf("not TLS on %s", addr)
	}
	st := tc.ConnectionState()
	if len(st.PeerCertificates) == 0 {
		return nil, fmt.Errorf("no certificate on %s", addr)
	}
	return st.PeerCertificates, nil
}
