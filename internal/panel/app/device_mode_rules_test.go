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

// Unbind rules and the device mode travel the API: settings round-trip with validation,
// tariffs keep or take the mode, users inherit it and may override it, and the page
// enforces the limit.
func TestUnbindRulesAndDeviceModeOverHTTP(t *testing.T) {
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
		t.Fatalf("login: %d", resp.StatusCode)
	}
	csrf := map[string]string{"X-CSRF-Token": h.csrf}
	api := "/" + adminPath + "/api/v1"

	getSettings := func() map[string]any {
		t.Helper()
		resp, body := h.do(http.MethodGet, api+"/settings", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("settings: %d", resp.StatusCode)
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := getSettings()["device_unbind"]; got == nil {
		t.Fatal("no device_unbind in settings")
	} else if m := got.(map[string]any); m["limit"].(float64) != 1 || m["days"].(float64) != 1 || m["return_hours"].(float64) != 24 {
		t.Fatalf("default rules: %v", m)
	}
	rules := map[string]any{"limit": 2, "days": 7, "return_hours": 6}
	if resp, _ := h.do(http.MethodPatch, api+"/settings", map[string]any{"device_unbind": rules}, csrf); resp.StatusCode != http.StatusOK {
		t.Fatalf("set rules: %d", resp.StatusCode)
	}
	if got := getSettings()["device_unbind"].(map[string]any); got["limit"].(float64) != 2 || got["days"].(float64) != 7 || got["return_hours"].(float64) != 6 {
		t.Fatalf("rules after save: %v", got)
	}
	if resp, body := h.do(http.MethodPatch, api+"/settings", map[string]any{"device_unbind": map[string]any{"limit": 101, "days": 7}}, csrf); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "bad_unbind_limit") {
		t.Fatalf("bad rules: %d %s", resp.StatusCode, body)
	}
	if resp, body := h.do(http.MethodPatch, api+"/settings", map[string]any{"device_unbind": map[string]any{"limit": 1, "days": 7, "return_hours": 721}}, csrf); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "bad_return_hours") {
		t.Fatalf("bad return: %d %s", resp.StatusCode, body)
	}

	tariffs, _ := h.st.Q.ListTariffs(ctx)
	tid := strconv.FormatInt(tariffs[1].ID, 10)
	if resp, body := h.do(http.MethodPut, api+"/tariffs/"+tid, map[string]any{"name": "Std", "duration_days": 30, "reset_strategy": "period", "device_mode": "online"}, csrf); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"device_mode":"online"`) {
		t.Fatalf("tariff mode: %d %s", resp.StatusCode, body)
	}
	if resp, body := h.do(http.MethodPut, api+"/tariffs/"+tid, map[string]any{"name": "Std", "duration_days": 30, "reset_strategy": "period", "device_mode": "sideways"}, csrf); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "bad_device_mode") {
		t.Fatalf("junk tariff mode: %d %s", resp.StatusCode, body)
	}
	if resp, body := h.do(http.MethodPost, api+"/users", map[string]any{"name": "a", "tariff_id": tariffs[1].ID}, csrf); resp.StatusCode != http.StatusCreated || !strings.Contains(string(body), `"device_mode":"online"`) {
		t.Fatalf("user inherits mode: %d %s", resp.StatusCode, body)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	resp, body := h.do(http.MethodPost, api+"/users", map[string]any{"name": "b", "tariff_id": tariffs[1].ID}, csrf)
	if resp.StatusCode != http.StatusCreated || json.Unmarshal(body, &created) != nil {
		t.Fatalf("create user: %d %s", resp.StatusCode, body)
	}
	uid := strconv.FormatInt(created.ID, 10)
	if resp, body := h.do(http.MethodPatch, api+"/users/"+uid, map[string]any{"device_mode": "bound"}, csrf); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"device_mode":"bound"`) {
		t.Fatalf("user mode patch: %d %s", resp.StatusCode, body)
	}
	if resp, body := h.do(http.MethodPatch, api+"/users/"+uid, map[string]any{"device_mode": "sideways"}, csrf); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "bad_device_mode") {
		t.Fatalf("junk user mode: %d %s", resp.StatusCode, body)
	}
}
