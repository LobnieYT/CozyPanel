package certcheck

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/netip"
	"testing"
	"time"

	"cozy/internal/panel/acme"
)

// cert is a real self-signed certificate, so a chain of it verifies.
func cert(t *testing.T, name string, days int) *x509.Certificate {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
		DNSNames:              []string{name},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Duration(days) * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type fakeDNS map[string][]netip.Addr

func (f fakeDNS) Lookup(_ context.Context, name string) ([]netip.Addr, error) {
	a, ok := f[name]
	if !ok {
		return nil, errors.New("no such name")
	}
	return a, nil
}

func find(r CertReport, id string) CertCheck {
	for _, c := range r.Checks {
		if c.ID == id {
			return c
		}
	}
	return CertCheck{ID: id, Status: "missing"}
}

// The panel's port answers with the very certificate the panel serves: ok. Something
// else answering is a fail the admin can fix by putting the certificate again.
func TestServedChecksTheCertificate(t *testing.T) {
	ctx := context.Background()
	own := cert(t, "vpn.example.com", 40)
	now := time.Unix(1_800_000_000, 0)
	// The synthetic certificate is its own root, so the chain verifies in the test.
	roots := x509.NewCertPool()
	roots.AddCert(own)
	c := &Checker{
		DNS:   fakeDNS{"vpn.example.com": {netip.MustParseAddr("203.0.113.10")}},
		Roots: roots,
		Now:   func() time.Time { return now },
	}
	c.Dial = func(_ context.Context, addr, sni string) ([]*x509.Certificate, error) {
		if addr != "203.0.113.10:21355" {
			t.Fatalf("dialled %s", addr)
		}
		return []*x509.Certificate{own}, nil
	}
	in := Input{Domain: "vpn.example.com", Host: "203.0.113.10", Ports: []int{21355}, Expected: own}
	r := c.Run(ctx, in)
	if got := find(r, "served_21355"); got.Status != OK {
		t.Fatalf("the right certificate: %+v", got)
	}
	if got := find(r, "domain"); got.Status != OK {
		t.Fatalf("the domain leads here: %+v", got)
	}
	// Another certificate answers: the admin has to put theirs again.
	other := cert(t, "someone.example", 40)
	c.Dial = func(context.Context, string, string) ([]*x509.Certificate, error) {
		return []*x509.Certificate{other}, nil
	}
	got := find(c.Run(ctx, in), "served_21355")
	if got.Status != Fail || got.Fix != FixSetCert {
		t.Fatalf("a stranger's certificate: %+v", got)
	}
	// Nothing answers at all.
	c.Dial = func(context.Context, string, string) ([]*x509.Certificate, error) {
		return nil, errors.New("connection refused")
	}
	if got := find(c.Run(ctx, in), "served_21355"); got.Status != Fail {
		t.Fatalf("no answer: %+v", got)
	}
	// An expired certificate is a fail with the renew button: the panel really serves it.
	expired := cert(t, "vpn.example.com", -1)
	in.Expected = expired
	c.Dial = func(context.Context, string, string) ([]*x509.Certificate, error) {
		return []*x509.Certificate{expired}, nil
	}
	if got := find(c.Run(ctx, in), "served_21355"); got.Status != Fail || got.Fix != FixRetry {
		t.Fatalf("expired: %+v", got)
	}
}

// The findings come worst first, so the admin reads the broken thing first.
func TestReportRanksWorstFirst(t *testing.T) {
	ctx := context.Background()
	own := cert(t, "vpn.example.com", 40)
	now := time.Unix(1_800_000_000, 0)
	c := &Checker{
		DNS: fakeDNS{"vpn.example.com": {netip.MustParseAddr("198.51.100.9")}},
		Now: func() time.Time { return now },
		Dial: func(context.Context, string, string) ([]*x509.Certificate, error) {
			return []*x509.Certificate{own}, nil
		},
	}
	in := Input{Domain: "vpn.example.com", Host: "203.0.113.10", Ports: []int{21355}, Expected: own,
		Status: acmeSelfSigned}
	r := c.Run(ctx, in)
	if len(r.Checks) < 3 {
		t.Fatalf("too few findings: %+v", r.Checks)
	}
	for i := 1; i < len(r.Checks); i++ {
		if rank(r.Checks[i].Status) < rank(r.Checks[i-1].Status) {
			t.Fatalf("out of order: %s before %s", r.Checks[i-1].Status, r.Checks[i].Status)
		}
	}
	if got := find(r, "domain"); got.Status != Warn || got.Fix != FixDomain {
		t.Fatalf("the domain leads elsewhere: %+v", got)
	}
	if got := find(r, "acme"); got.Status != Warn {
		t.Fatalf("self-signed: %+v", got)
	}
	// The port 80 check only runs with a responder.
	if got := find(r, "port80"); got.ID == "port80" && got.Status != "missing" {
		t.Fatal("port 80 checked without a responder")
	}
}

// Port 80: with a responder the check reports whether the port can be bound.
func TestPort80Check(t *testing.T) {
	c := &Checker{Now: time.Now}
	if got := c.port80(context.Background(), Input{}); got != nil {
		t.Fatalf("no responder, no check: %+v", got)
	}
	if got := c.port80(context.Background(), Input{Challenge: responder{present: false}}); got != nil {
		t.Fatalf("an absent responder, no check: %+v", got)
	}
	got := c.port80(context.Background(), Input{Challenge: responder{present: true}})
	if len(got) != 1 || got[0].ID != "port80" {
		t.Fatalf("the port 80 finding: %+v", got)
	}
	if got[0].Status != Fail && got[0].Fix != FixPort80 {
		t.Fatalf("the port 80 finding: %+v", got[0])
	}
}

type responder struct{ present bool }

func (r responder) Present() bool { return r.present }

// The panel's own certificate story, as the status carries it.
func statusOf(kind string) acme.Status { return acme.Status{Kind: kind, Trusted: kind == "custom"} }

// acmeSelfSigned is the panel's status without a certificate.
var acmeSelfSigned = acme.Status{Kind: "self-signed"}

func statusBroken() acme.Status { return acme.Status{Kind: "self-signed", Error: "port 80 is busy"} }

func TestACMEStatusKinds(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want string
	}{
		{"letsencrypt", OK},
		{"custom", Info},
		{"self-signed", Warn},
	} {
		got := acmeStatus(statusOf(tc.kind))
		if got[0].Status != tc.want {
			t.Fatalf("%s: %+v, want %s", tc.kind, got[0], tc.want)
		}
	}
	// An ACME error is a fail with the renew button whatever the kind is.
	got := acmeStatus(statusOf("self-signed"))
	if got[0].Fix != FixNone {
		t.Fatalf("a self-signed panel has nothing to fix: %+v", got[0])
	}
	broken := acmeStatus(statusBroken())
	if len(broken) != 2 || broken[1].Status != Fail || broken[1].Fix != FixRetry {
		t.Fatalf("a broken certificate: %+v", broken)
	}
}
