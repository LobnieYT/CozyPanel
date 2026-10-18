package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"cozy/internal/panel/store/db"
)

// A node nothing listens on: the report says so first, with the fix for the address, and
// does not pretend the rest of the node can be checked.
func TestCheckNodeUnreachable(t *testing.T) {
	h := newHarness(t)
	h.login(password, "")
	n, err := h.st.Q.CreateNode(context.Background(), db.CreateNodeParams{
		Name: "far", Address: "127.0.0.1:1", PublicHost: "far.example", Domain: "far.example",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, out := h.do(http.MethodPost, "/"+adminPath+"/api/v1/nodes/"+strconv.FormatInt(n.ID, 10)+"/check", nil, map[string]string{"X-CSRF-Token": h.csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	var rep struct {
		Checks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Fix    string `json:"fix"`
		} `json:"checks"`
		CheckedAt time.Time `json:"checked_at"`
	}
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.CheckedAt.IsZero() {
		t.Fatal("no timestamp")
	}
	if len(rep.Checks) == 0 {
		t.Fatalf("empty report: %s", out)
	}
	first := rep.Checks[0]
	if first.ID != "address" || first.Status != "fail" {
		t.Fatalf("first check: %+v", first)
	}
	if first.Fix != "node-api" {
		t.Fatalf("a closed port means the node's API is not listening: %+v", first)
	}
	for _, c := range rep.Checks[1:] {
		if c.ID == "key" || c.ID == "inbounds" {
			t.Fatalf("checked %s of a node that never answered", c.ID)
		}
	}
}

// The check needs a session: without one it is not even a hint about a node.
func TestCheckNodeNeedsSession(t *testing.T) {
	h := newHarness(t)
	n, err := h.st.Q.CreateNode(context.Background(), db.CreateNodeParams{
		Name: "far", Address: "127.0.0.1:1", PublicHost: "far.example",
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := h.do(http.MethodPost, "/"+adminPath+"/api/v1/nodes/"+strconv.FormatInt(n.ID, 10)+"/check", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
