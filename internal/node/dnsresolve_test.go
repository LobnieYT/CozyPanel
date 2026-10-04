package node

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"

	"cozy/internal/nodeapi"
)

// A local stub authoritative server: the resolve test must prove itself
// without the internet.
func stubDNS(t *testing.T, ip string) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, m *dns.Msg) {
		r := new(dns.Msg).SetReply(m)
		r.Answer = append(r.Answer, &dns.A{Hdr: dns.RR_Header{Name: m.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP(ip)})
		_ = w.WriteMsg(r)
	})}
	go func() { _ = srv.ActivateAndServe() }()
	t.Cleanup(func() { _ = srv.Shutdown() })
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func TestDNSResolve(t *testing.T) {
	port := stubDNS(t, "203.0.113.9")
	e := &Engine{applied: nodeapi.DesiredState{DNS: &nodeapi.NodeDNS{Enable: true, Servers: []nodeapi.DNSServer{
		{Address: "127.0.0.1", Port: port},
		{Address: "system://"},
	}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := e.DNSResolve(ctx, nodeapi.DNSResolveRequest{Domain: "gemini.google.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Answers) != 2 {
		t.Fatalf("answers: %+v", res.Answers)
	}
	if res.Answers[0].Error != "" || len(res.Answers[0].IPs) != 1 || res.Answers[0].IPs[0] != "203.0.113.9" {
		t.Errorf("plain: %+v", res.Answers[0])
	}
	if res.Answers[1].Error == "" {
		t.Errorf("system must be unsupported: %+v", res.Answers[1])
	}
	e.applied.DNS.Enable = false
	if res, err := e.DNSResolve(ctx, nodeapi.DNSResolveRequest{Domain: "example.com"}); err != nil || len(res.Answers) != 0 {
		t.Fatalf("disabled: %+v %v", res, err)
	}
}
