package nodecheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// A refused connection means the node's API is not listening — not that the panel is wrong.
func TestClassifyRefusedMeansAPI(t *testing.T) {
	c := classifyError(errors.New("dial tcp 10.0.0.5:2222: connect: connection refused"))
	if c.Status != Fail || c.Detail != "refused" || c.Fix != FixNodeAPI {
		t.Fatalf("got %+v", c)
	}
}

// A DNS failure is the address, and the panel says so instead of "unavailable".
func TestClassifyDNS(t *testing.T) {
	c := classifyError(errors.New("dial tcp: lookup node.example: no such host"))
	if c.Detail != "dns" || c.Fix != FixNodeAddress {
		t.Fatalf("got %+v", c)
	}
}

// A certificate complaint must point at the panel's certificate, the usual cause: the node
// answers with the self-signed one.
func TestClassifyTLSPointsAtPanelCert(t *testing.T) {
	c := classifyError(errors.New("remote error: tls: bad certificate"))
	if c.Status != Fail || c.Detail != "tls" || c.Fix != FixPanelCert {
		t.Fatalf("got %+v", c)
	}
}

// A timeout and a reset are different: one is a wrong address, the other a firewall.
func TestClassifyTimeoutAndReset(t *testing.T) {
	if got := classifyError(context.DeadlineExceeded); got.Fix != FixNodeAddress {
		t.Fatalf("timeout: %+v", got)
	}
	if got := classifyError(errors.New("read: connection reset by peer")); got.Status != Warn || got.Fix != FixFirewall {
		t.Fatalf("reset: %+v", got)
	}
}

// An unknown error is a failure but claims no fix, so the panel never sends the admin to
// a wrong button.
func TestClassifyUnknownHasNoFix(t *testing.T) {
	if got := classifyError(errors.New("something odd")); got.Status != Fail || got.Fix != FixNone {
		t.Fatalf("got %+v", got)
	}
}

// The worst finding comes first: the admin reads the broken thing before the fine ones.
func TestRankPutsFailFirst(t *testing.T) {
	r := report([]NodeCheck{{ID: "ok", Status: OK}, {ID: "warn", Status: Warn}, {ID: "fail", Status: Fail}})
	if r.Checks[0].ID != "fail" || r.Checks[1].ID != "warn" || r.Checks[2].ID != "ok" {
		t.Fatalf("got %+v", r.Checks)
	}
	if r.CheckedAt.IsZero() {
		t.Fatal("no timestamp")
	}
}

// A node that answers with no listener works, but nobody can use it: warn, not ok.
func TestNoInboundIsWarn(t *testing.T) {
	r := Check(context.Background(), Node{ID: 1, Address: "", Inbounds: 0}, nil)
	var got NodeCheck
	for _, c := range r.Checks {
		if c.ID == "inbounds" {
			got = c
		}
	}
	if got.Status != Warn || got.Fix != FixInbound {
		t.Fatalf("got %+v", got)
	}
}

// A node the panel cannot dial at all: the address check fails and nothing pretends it works.
func TestUnreachableNodeFailsAddress(t *testing.T) {
	// 127.0.0.1:1 is closed on every machine.
	r := Check(context.Background(), Node{ID: 1, Address: "127.0.0.1:1"}, nil)
	if r.Checks[0].ID != "address" || r.Checks[0].Status != Fail {
		t.Fatalf("got %+v", r.Checks)
	}
	for _, c := range r.Checks {
		if c.ID == "inbounds" || c.ID == "key" {
			t.Fatalf("checked %s without an answer", c.ID)
		}
	}
}

// A listening node with the wrong key: auth, and the fix is the key.
func TestWrongKeyIsAuth(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	r := Check(context.Background(), Node{ID: 1, Address: ln.Addr().String(), KeysOK: false, Inbounds: 2}, nil)
	found := map[string]NodeCheck{}
	for _, c := range r.Checks {
		found[c.ID] = c
	}
	if found["address"].Status != OK {
		t.Fatalf("address: %+v", found["address"])
	}
	if found["key"].Detail != "auth" || found["key"].Fix != FixKey {
		t.Fatalf("key: %+v", found["key"])
	}
	if found["inbounds"].Status != OK || !strings.Contains(found["inbounds"].Detail, "2") {
		t.Fatalf("inbounds: %+v", found["inbounds"])
	}
}

// The ask callback's error is what the answer check reports.
func TestAskErrorBecomesAnswer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	r := Check(context.Background(), Node{ID: 1, Address: ln.Addr().String(), KeysOK: true, Inbounds: 1},
		func(context.Context) error { return errors.New("unauthorized") })
	for _, c := range r.Checks {
		if c.ID == "answer" {
			if c.Detail != "auth" {
				t.Fatalf("got %+v", c)
			}
			return
		}
	}
	t.Fatal("no answer check")
}

// The panel's own node has no address to dial, and that is fine.
func TestLocalNodeHasNothingToDial(t *testing.T) {
	r := Check(context.Background(), Node{ID: 1, Local: true, KeysOK: true, Inbounds: 3}, nil)
	for _, c := range r.Checks {
		if c.ID == "address" && c.Status != OK {
			t.Fatalf("got %+v", c)
		}
	}
}
