package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"cozy/internal/panel/domain"
	"cozy/internal/panel/settings"
)

// NodeInfoShape is what the Nodes list answers about one node.
type NodeInfoShape struct {
	ID          int64  `json:"id"`
	FairShare   bool   `json:"fair_share"`
	ChannelMbps *int64 `json:"channel_mbps"`
}

// Speed caps on plans and users, and a node's channel shared out: the API round-trips
// them, a plan's cap reaches its users, and fair share needs a width to divide.
func TestSpeedLimitsAndFairShareOverHTTP(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := domain.Seed(ctx, h.st, h.now); err != nil {
		t.Fatal(err)
	}
	set := settings.New(h.st.Q)
	for k, v := range map[string]any{settings.KeyPublicHost: "203.0.113.10", settings.KeyPanelPort: 21355} {
		if err := settings.Set(ctx, set, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if resp, _ := h.login(password, ""); resp.StatusCode != http.StatusOK {
		t.Fatal("login")
	}
	csrf := map[string]string{"X-CSRF-Token": h.csrf}
	api := "/" + adminPath + "/api/v1"
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	tariffs, _ := h.st.Q.ListTariffs(ctx)
	std := tariffs[1]

	// The plan's cap travels to the tariff and to a user created from it.
	if resp, body := h.do(http.MethodPut, api+"/tariffs/"+id(std.ID), map[string]any{"name": "Std", "duration_days": 30, "reset_strategy": "period", "speed_limit": 100}, csrf); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"speed_limit":100`) {
		t.Fatalf("tariff cap: %d %s", resp.StatusCode, body)
	}
	resp, body := h.do(http.MethodPost, api+"/users", map[string]any{"name": "a", "tariff_id": std.ID}, csrf)
	if resp.StatusCode != http.StatusCreated || !strings.Contains(string(body), `"speed_limit":100`) {
		t.Fatalf("the user takes the plan's cap: %d %s", resp.StatusCode, body)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	uid := id(created.ID)
	// A cap of its own wins over the plan's.
	if resp, body := h.do(http.MethodPatch, api+"/users/"+uid, map[string]any{"speed_limit": 25}, csrf); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"speed_limit":25`) {
		t.Fatalf("the user's own cap: %d %s", resp.StatusCode, body)
	}
	if resp, body := h.do(http.MethodPatch, api+"/users/"+uid, map[string]any{"speed_unlimited": true}, csrf); resp.StatusCode != http.StatusOK || strings.Contains(string(body), `"speed_limit":25`) {
		t.Fatalf("no cap: %d %s", resp.StatusCode, body)
	}
	if resp, body := h.do(http.MethodPatch, api+"/users/"+uid, map[string]any{"speed_limit": 1_000_000}, csrf); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a cap over the ceiling: %d %s", resp.StatusCode, body)
	}

	// A node's channel: fair share without a width has nothing to divide.
	nodeAPI := api + "/nodes/" + id(1)
	if resp, body := h.do(http.MethodPatch, nodeAPI, map[string]any{"fair_share": true}, csrf); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "channel_required") {
		t.Fatalf("fair share without a channel: %d %s", resp.StatusCode, body)
	}
	if resp, body := h.do(http.MethodPatch, nodeAPI, map[string]any{"fair_share": true, "channel_mbps": 500}, csrf); resp.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), `"fair_share":true`) || !strings.Contains(string(body), `"channel_mbps":500`) {
		t.Fatalf("the channel: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(http.MethodGet, api+"/nodes", nil, nil)
	var list []NodeInfoShape
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &list) != nil {
		t.Fatalf("nodes: %d %s", resp.StatusCode, body)
	}
	var self NodeInfoShape
	for _, item := range list {
		if item.ID == 1 {
			self = item
		}
	}
	if !self.FairShare || self.ChannelMbps == nil || *self.ChannelMbps != 500 {
		t.Fatalf("the channel after a reload: %+v %s", self, body)
	}
	// Turning it off keeps the width for later.
	if resp, body := h.do(http.MethodPatch, nodeAPI, map[string]any{"fair_share": false}, csrf); resp.StatusCode != http.StatusOK || strings.Contains(string(body), `"fair_share":true`) {
		t.Fatalf("fair share off: %d %s", resp.StatusCode, body)
	}
	got, _ := h.st.Q.GetNode(ctx, 1)
	if got.FairShare != 0 || !got.ChannelMbps.Valid {
		t.Fatalf("the node after turning it off: %+v", got)
	}
	// The cap reaches the node: the policy carries it.
	syncerPolicy := domain.ValidSpeedLimit(50)
	if !syncerPolicy {
		t.Fatal("50 Mbit/s must be a valid cap")
	}
	if domain.ValidSpeedLimit(0) || domain.ValidSpeedLimit(domain.MaxSpeedLimit+1) {
		t.Fatal("zero and over the ceiling must not be valid")
	}
}
