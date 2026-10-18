// Package nodecheck says, in plain words, why a node is not connecting: the panel asks
// the node, or tries itself what it can, and every finding is a code the admin panel
// translates with what to do about it.
package nodecheck

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// The outcomes of a check: ok, warn (works, but not everywhere), fail (does not work).
const (
	OK   = "ok"
	Warn = "warn"
	Fail = "fail"
)

// The fixes the panel offers as a button, or a command to copy.
const (
	FixNone        = ""
	FixNodeAddress = "node-address"
	FixPanelCert   = "panel-certificate"
	FixFirewall    = "node-firewall"
	FixClock       = "node-clock"
	FixNodeAPI     = "node-api"
	FixInbound     = "node-inbound"
	FixKey         = "node-key"
	FixContainer   = "node-container"
)

// NodeCheck is one finding.
type NodeCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

// NodeCheckReport is every finding, the worst first.
type NodeCheckReport struct {
	Checks    []NodeCheck `json:"checks"`
	CheckedAt time.Time   `json:"checked_at"`
}

// Node is what to check.
type Node struct {
	ID      int64
	Name    string
	Address string // host:port of its API
	Host    string // what clients dial
	SNI     string // its domain, "" on an IP
	Local   bool
	// Expected is the panel certificate the node must answer with; nil skips that check.
	Expected []byte
	// Health is what the node said about itself, when it answered at all.
	HealthErr error
	// Inbounds is how many listeners the node has, as the panel last saw them.
	Inbounds int
	// Keys is how many the panel's own key opened on the node.
	KeysOK bool
}

// classifyError is the plain reason an error carries: the panel shows the code, not the
// text of a Go error.
func classifyError(err error) NodeCheck {
	if err == nil {
		return NodeCheck{Status: OK}
	}
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "timeout"), strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "context deadline"):
		return NodeCheck{Status: Fail, Detail: "timeout", Fix: FixNodeAddress}
	case strings.Contains(msg, "connection refused"):
		return NodeCheck{Status: Fail, Detail: "refused", Fix: FixNodeAPI}
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "name resolution"), strings.Contains(msg, "dns"):
		return NodeCheck{Status: Fail, Detail: "dns", Fix: FixNodeAddress}
	case strings.Contains(msg, "certificate"), strings.Contains(msg, "tls:"), strings.Contains(msg, "x509"):
		return NodeCheck{Status: Fail, Detail: "tls", Fix: FixPanelCert}
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"), strings.Contains(msg, "401"), strings.Contains(msg, "403"):
		return NodeCheck{Status: Fail, Detail: "auth", Fix: FixKey}
	case strings.Contains(msg, "no route to host"), strings.Contains(msg, "network is unreachable"):
		return NodeCheck{Status: Fail, Detail: "unreachable", Fix: FixFirewall}
	case strings.Contains(msg, "connection reset"):
		return NodeCheck{Status: Warn, Detail: "reset", Fix: FixFirewall}
	}
	return NodeCheck{Status: Fail, Detail: "unavailable"}
}

// CheckNode asks the node about itself and reports what the panel sees: the address, the
// certificate, the key, and the listeners the panel's last state had. ask is what the
// panel's node client says when it calls the node; nil skips that call.
func Check(ctx context.Context, n Node, ask func(context.Context) error) NodeCheckReport {
	var out []NodeCheck
	add := func(cs ...NodeCheck) { out = append(out, cs...) }

	// The address: can the panel even reach it. If not, nothing after it can be told.
	addr := n.Address
	if addr == "" {
		add(NodeCheck{ID: "address", Status: OK, Detail: "the panel's own node"})
	} else if err := dialable(ctx, addr); err != nil {
		c := classifyError(err)
		c.ID = "address"
		add(c)
		return report(out)
	} else {
		add(NodeCheck{ID: "address", Status: OK})
	}

	// The node's answer: up or not.
	if n.HealthErr == nil && addr != "" && ask != nil {
		if err := ask(ctx); err != nil {
			n.HealthErr = err
		}
	}
	if n.HealthErr != nil {
		c := classifyError(n.HealthErr)
		c.ID = "answer"
		add(c)
	} else if addr != "" {
		add(NodeCheck{ID: "answer", Status: OK})
	}

	// The key: the panel's certificate opened the API.
	if addr != "" && n.HealthErr == nil {
		if n.KeysOK {
			add(NodeCheck{ID: "key", Status: OK})
		} else {
			add(NodeCheck{ID: "key", Status: Fail, Detail: "auth", Fix: FixKey})
		}
	}

	// The listeners: a node with none serves nobody.
	if n.HealthErr == nil {
		switch {
		case n.Inbounds == 0:
			add(NodeCheck{ID: "inbounds", Status: Warn, Detail: "no listener", Fix: FixInbound})
		default:
			add(NodeCheck{ID: "inbounds", Status: OK, Detail: fmt.Sprint(n.Inbounds)})
		}
	}
	return report(out)
}

// dialable opens and closes a TCP connection: enough to tell "nothing listens" from "the
// host is not there".
func dialable(ctx context.Context, addr string) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// tlsCheck is what a node presents for its own address.
func tlsCheck(ctx context.Context, host, sni string) NodeCheck {
	d := &tls.Dialer{Config: &tls.Config{ServerName: sni, InsecureSkipVerify: true}, //nolint:gosec // the chain itself is the finding
		NetDialer: &net.Dialer{Timeout: 5 * time.Second}}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		c := classifyError(err)
		c.ID = "tls"
		return c
	}
	defer conn.Close()
	return NodeCheck{ID: "tls", Status: OK}
}

func report(cs []NodeCheck) NodeCheckReport {
	return NodeCheckReport{Checks: rank(cs), CheckedAt: time.Now()}
}

// rank puts what fails first: the admin reads the broken thing first.
func rank(cs []NodeCheck) []NodeCheck {
	out := make([]NodeCheck, len(cs))
	copy(out, cs)
	score := func(s string) int {
		switch s {
		case Fail:
			return 0
		case Warn:
			return 1
		}
		return 2
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && score(out[j].Status) < score(out[j-1].Status); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
