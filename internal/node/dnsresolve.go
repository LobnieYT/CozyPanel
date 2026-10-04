package node

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"cozy/internal/nodeapi"
)

// resolveTimeout bounds one server's answer: a hanging upstream must not hold
// the whole test.
const resolveTimeout = 10 * time.Second

// DNSResolve asks every effective DNS server for the domain in turn and reports
// what each answered: the ground truth behind "which DNS answered". Plain,
// udp/tcp, DoT and DoH travel directly from the node; system/dhcp ones have no
// address to ask and are reported as unsupported.
func (e *Engine) DNSResolve(ctx context.Context, req nodeapi.DNSResolveRequest) (nodeapi.DNSResolveResult, error) {
	e.mu.Lock()
	st := e.applied
	e.mu.Unlock()
	out := nodeapi.DNSResolveResult{}
	if st.DNS == nil || !st.DNS.Enable {
		return out, nil
	}
	domain := nodeapi.NormalizeDomain(req.Domain)
	if domain == "" {
		return out, nil
	}
	qtype := dns.TypeA
	if strings.EqualFold(req.Type, "AAAA") {
		qtype = dns.TypeAAAA
	}
	out.Answers = make([]nodeapi.DNSServerAnswer, len(st.DNS.Servers))
	var wg sync.WaitGroup
	for i, s := range st.DNS.Servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out.Answers[i] = exchangeServer(ctx, nodeapi.RenderAddress(s), domain, qtype)
		}()
	}
	wg.Wait()
	return out, nil
}

// exchangeServer asks one rendered server address for name records.
func exchangeServer(ctx context.Context, server, domain string, qtype uint16) nodeapi.DNSServerAnswer {
	ans := nodeapi.DNSServerAnswer{Server: server}
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	addr := server
	if !strings.Contains(addr, "://") {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, "53")
		}
		var err error
		ans.IPs, ans.RTTMs, err = exchangePlain(ctx, "udp", addr, domain, qtype)
		if err != nil {
			ans.Error = err.Error()
		}
		return ans
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		ans.Error = "bad server"
		return ans
	}
	switch strings.ToLower(u.Scheme) {
	case "udp", "tcp":
		host := u.Host
		if _, _, err := net.SplitHostPort(host); err != nil {
			host = net.JoinHostPort(u.Hostname(), "53")
		}
		ans.IPs, ans.RTTMs, err = exchangePlain(ctx, strings.ToLower(u.Scheme), host, domain, qtype)
	case "tls":
		host := u.Host
		if _, _, err := net.SplitHostPort(host); err != nil {
			host = net.JoinHostPort(u.Hostname(), "853")
		}
		ans.IPs, ans.RTTMs, err = exchangeTLS(ctx, host, u.Hostname(), domain, qtype)
	case "https", "http":
		ans.IPs, ans.RTTMs, err = exchangeDoH(ctx, addr, domain, qtype)
	default:
		err = fmt.Errorf("unsupported in test")
	}
	if err != nil {
		ans.Error = err.Error()
	}
	return ans
}

// exchangePlain asks a plain, udp:// or tcp:// upstream.
func exchangePlain(ctx context.Context, network, addr, domain string, qtype uint16) ([]string, int64, error) {
	m := new(dns.Msg).SetQuestion(dns.Fqdn(domain), qtype)
	c := &dns.Client{Net: network, Timeout: resolveTimeout}
	start := time.Now()
	r, _, err := c.ExchangeContext(ctx, m, addr)
	if err != nil {
		return nil, 0, err
	}
	return answerIPs(r, qtype), time.Since(start).Milliseconds(), nil
}

// exchangeTLS asks a tls:// upstream.
func exchangeTLS(ctx context.Context, addr, serverName, domain string, qtype uint16) ([]string, int64, error) {
	m := new(dns.Msg).SetQuestion(dns.Fqdn(domain), qtype)
	dialer := &net.Dialer{Timeout: resolveTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()
	dc := &dns.Conn{Conn: conn}
	if deadline, ok := ctx.Deadline(); ok {
		_ = dc.SetDeadline(deadline)
	}
	start := time.Now()
	if err := dc.WriteMsg(m); err != nil {
		return nil, 0, err
	}
	r, err := dc.ReadMsg()
	if err != nil {
		return nil, 0, err
	}
	return answerIPs(r, qtype), time.Since(start).Milliseconds(), nil
}

// exchangeDoH asks a https:// upstream with a POST, like mihomo does.
func exchangeDoH(ctx context.Context, endpoint, domain string, qtype uint16) ([]string, int64, error) {
	m := new(dns.Msg).SetQuestion(dns.Fqdn(domain), qtype)
	raw, err := m.Pack()
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("HTTP %s", resp.Status)
	}
	r := new(dns.Msg)
	if err := r.Unpack(body); err != nil {
		return nil, 0, err
	}
	if r.Rcode != dns.RcodeSuccess {
		return nil, 0, fmt.Errorf("rcode %s", dns.RcodeToString[r.Rcode])
	}
	return answerIPs(r, qtype), time.Since(start).Milliseconds(), nil
}

// answerIPs pulls the A/AAAA answers out of a response.
func answerIPs(r *dns.Msg, qtype uint16) []string {
	var out []string
	if r == nil {
		return out
	}
	for _, rr := range r.Answer {
		switch v := rr.(type) {
		case *dns.A:
			if qtype == dns.TypeA {
				out = append(out, v.A.String())
			}
		case *dns.AAAA:
			if qtype == dns.TypeAAAA {
				out = append(out, v.AAAA.String())
			}
		}
	}
	return out
}
