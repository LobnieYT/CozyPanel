package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"cozy/internal/panel/domain"
	"cozy/internal/panel/settings"
)

// Devices bound to a subscription, end to end over HTTP: own keys per device, a stub
// once the places are taken, the subscription page's list and unbind (same site only,
// once a day), the admin's list with ids and unlimited unbind.
func TestDeviceBindingOverHTTP(t *testing.T) {
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
	clock := func() time.Time { return h.now }
	tariffs, _ := h.st.Q.ListTariffs(ctx)
	u, err := domain.NewUsers(h.st, domain.NewPool(h.st, clock), noChanges{}, clock).Create(ctx, domain.CreateInput{Name: "a", TariffID: tariffs[1].ID}) // 3 devices
	if err != nil {
		t.Fatal(err)
	}
	own, _ := h.st.Q.GetSlot(ctx, u.SlotID.Int64)
	sub := "/" + subPath + "/" + u.SubToken
	fetch := func(hwid string) (*http.Response, string) {
		hdr := map[string]string{"User-Agent": "Happ/3.4.1"}
		if hwid != "" {
			hdr["X-Hwid"], hdr["X-Device-Os"], hdr["X-Device-Model"] = hwid, "Android", "Pixel 9"
		}
		resp, body := h.do(http.MethodGet, sub, nil, hdr)
		links, _ := base64.StdEncoding.DecodeString(string(body))
		return resp, string(links)
	}

	seen := map[string]bool{}
	for _, id := range []string{"phone-0123456789", "laptop-0123456789", "tablet-0123456789"} {
		resp, links := fetch(id)
		if resp.StatusCode != http.StatusOK || strings.Contains(links, own.Uuid) || strings.Contains(links, "127.0.0.1:1") {
			t.Fatalf("%s: %d, gets its own keys, not the user's: %.120s", id, resp.StatusCode, links)
		}
		uuid := links[strings.Index(links, "vless://")+8 : strings.Index(links, "vless://")+44]
		if seen[uuid] {
			t.Fatalf("%s shares keys with another device", id)
		}
		seen[uuid] = true
		if _, again := fetch(id); !strings.Contains(again, uuid) {
			t.Fatalf("%s: the same keys on every fetch", id)
		}
	}
	resp, links := fetch("fourth-0123456789")
	if resp.Header.Get("X-Hwid-Max-Devices-Reached") != "true" || !strings.Contains(links, "127.0.0.1:1") || strings.Count(links, "://") != 1 {
		t.Fatalf("a fourth device gets the stub: %v %q", resp.Header, links)
	}
	// The stub's name is the notice the app shows; it is in the panel's language.
	notice := func(links string) string {
		name, _ := url.PathUnescape(links[strings.LastIndex(links, "#")+1:])
		return name
	}
	if n := notice(links); !strings.HasPrefix(n, "⛔ Все места") {
		t.Fatalf("notice without a language: %q", n)
	}
	if err := settings.Set(ctx, set, settings.KeyDefaultLang, "en"); err != nil {
		t.Fatal(err)
	}
	if _, links := fetch("fourth-0123456789"); !strings.HasPrefix(notice(links), "⛔ All device places are taken") {
		t.Fatalf("notice in English: %q", notice(links))
	}

	resp, body := h.do(http.MethodGet, sub+"/info", nil, nil)
	var info struct {
		Binding bool `json:"binding"`
		Devices []struct {
			ID    int64  `json:"id"`
			Model string `json:"model"`
			Used  int64  `json:"used"`
		} `json:"devices"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &info) != nil || !info.Binding || len(info.Devices) != 3 || info.Devices[0].Model != "Pixel 9" {
		t.Fatalf("info: %d %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "phone-0123456789") {
		t.Fatal("the page must not show device ids")
	}
	for _, d := range info.Devices {
		if d.Used != 0 {
			t.Fatalf("uncounted device %d shows used %d", d.ID, d.Used)
		}
	}
	if _, err := h.st.DB.ExecContext(ctx, "INSERT INTO slot_traffic (slot_id, up, down) SELECT slot_id, 200, 300 FROM bound_devices WHERE hwid = 'phone-0123456789'"); err != nil {
		t.Fatal(err)
	}
	resp, body = h.do(http.MethodGet, sub+"/info", nil, nil)
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &info) != nil {
		t.Fatalf("info again: %d %s", resp.StatusCode, body)
	}
	withUsed := 0
	for _, d := range info.Devices {
		if d.Used != 0 {
			withUsed++
			if d.Used != 500 {
				t.Fatalf("device %d used %d, want 500", d.ID, d.Used)
			}
		}
	}
	if withUsed != 1 {
		t.Fatalf("devices with used traffic: %d, want 1", withUsed)
	}
	unbind := sub + "/devices/" + strconv.FormatInt(info.Devices[0].ID, 10) + "/unbind"
	if resp, _ := h.do(http.MethodPost, unbind, nil, map[string]string{"Sec-Fetch-Site": "cross-site"}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unbind from another site: %d", resp.StatusCode)
	}
	if resp, _ := h.do(http.MethodPost, unbind, nil, map[string]string{"Sec-Fetch-Site": "same-origin"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unbind from the page: %d", resp.StatusCode)
	}
	if resp, links := fetch("fourth-0123456789"); resp.Header.Get("X-Hwid-Max-Devices-Reached") != "" || strings.Contains(links, "127.0.0.1:1") {
		t.Fatalf("the freed place: %q", links)
	}
	second := sub + "/devices/" + strconv.FormatInt(info.Devices[1].ID, 10) + "/unbind"
	if resp, _ := h.do(http.MethodPost, second, nil, map[string]string{"Sec-Fetch-Site": "same-origin"}); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a second unbind the same day: %d", resp.StatusCode)
	}

	if resp, _ := h.login(password, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	api := "/" + adminPath + "/api/v1/users/" + strconv.FormatInt(u.ID, 10) + "/bound-devices"
	resp, body = h.do(http.MethodGet, api, nil, nil)
	var bound []struct {
		ID          int64  `json:"id"`
		HWID        string `json:"hwid"`
		TrafficUp   int64  `json:"traffic_up"`
		TrafficDown int64  `json:"traffic_down"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &bound) != nil || len(bound) != 3 || bound[0].HWID == "" {
		t.Fatalf("admin list: %d %s", resp.StatusCode, body)
	}
	for _, b := range bound {
		if b.TrafficUp != 0 || b.TrafficDown != 0 {
			t.Fatalf("uncounted device %d shows traffic: up %d down %d", b.ID, b.TrafficUp, b.TrafficDown)
		}
	}
	if _, err := h.st.DB.ExecContext(ctx, "INSERT INTO slot_traffic (slot_id, up, down) SELECT slot_id, 150, 950 FROM bound_devices WHERE id = ?", bound[0].ID); err != nil {
		t.Fatal(err)
	}
	resp, body = h.do(http.MethodGet, api, nil, nil)
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &bound) != nil {
		t.Fatalf("admin list again: %d %s", resp.StatusCode, body)
	}
	for _, b := range bound {
		wantUp, wantDown := int64(0), int64(0)
		if b.ID == bound[0].ID {
			wantUp, wantDown = 150, 950
		}
		if b.TrafficUp != wantUp || b.TrafficDown != wantDown {
			t.Fatalf("device %d: up %d down %d, want %d %d", b.ID, b.TrafficUp, b.TrafficDown, wantUp, wantDown)
		}
	}
	if resp, _ := h.do(http.MethodDelete, api+"/"+strconv.FormatInt(bound[0].ID, 10), nil, map[string]string{"X-CSRF-Token": h.csrf}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("the admin unbinds without the daily limit: %d", resp.StatusCode)
	}

	rename := api + "/" + strconv.FormatInt(bound[1].ID, 10)
	if resp, body := h.do(http.MethodPatch, rename, map[string]string{"name": "  Планшет  "}, map[string]string{"X-CSRF-Token": h.csrf}); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"name":"Планшет"`) {
		t.Fatalf("rename: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.do(http.MethodPatch, rename, map[string]string{"name": strings.Repeat("a", 41)}, map[string]string{"X-CSRF-Token": h.csrf}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a long name: %d", resp.StatusCode)
	}
	resp, body = h.do(http.MethodGet, api, nil, nil)
	var named []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal(body, &named) != nil {
		t.Fatalf("admin list: %s", body)
	}
	for _, b := range named {
		if b.ID == bound[1].ID && b.Name != "Планшет" {
			t.Fatalf("the name in the list: %q", b.Name)
		}
	}

	ban := api + "/" + strconv.FormatInt(bound[2].ID, 10) + "/ban"
	resp, body = h.do(http.MethodPost, ban, nil, map[string]string{"X-CSRF-Token": h.csrf})
	var banned []struct {
		ID    int64  `json:"id"`
		HWID  string `json:"hwid"`
		Label string `json:"label"`
	}
	if resp.StatusCode != http.StatusCreated || json.Unmarshal(body, &banned) != nil || len(banned) != 1 || banned[0].HWID == "" {
		t.Fatalf("ban: %d %s", resp.StatusCode, body)
	}
	bansAPI := "/" + adminPath + "/api/v1/users/" + strconv.FormatInt(u.ID, 10) + "/device-bans"
	resp, body = h.do(http.MethodGet, bansAPI, nil, nil)
	var bans []struct {
		ID    int64   `json:"id"`
		Label string  `json:"label"`
		Admin string  `json:"admin"`
		Until *string `json:"until"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &bans) != nil || len(bans) != 2 {
		t.Fatalf("bans: %d %s", resp.StatusCode, body)
	}
	// The admin's ban and the subscriber's own unbind pause live side by side.
	seenBan, seenPause := false, false
	for _, b := range bans {
		if b.Admin != "" && b.Until == nil {
			seenBan = true
		}
		if b.Admin == "" && b.Until != nil {
			seenPause = true
		}
	}
	if !seenBan || !seenPause {
		t.Fatalf("ban and pause: %s", body)
	}
	if resp, links := fetch(bound[2].HWID); resp.StatusCode != http.StatusOK || !strings.Contains(links, "127.0.0.1:1") {
		t.Fatalf("a banned device fetches keys: %d %.80s", resp.StatusCode, links)
	}
	if resp, _ := h.do(http.MethodDelete, bansAPI+"/"+strconv.FormatInt(bans[0].ID, 10), nil, map[string]string{"X-CSRF-Token": h.csrf}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unban: %d", resp.StatusCode)
	}
	if resp, links := fetch(bound[2].HWID); resp.StatusCode != http.StatusOK || strings.Contains(links, "127.0.0.1:1") {
		t.Fatalf("after the unban: %d %.80s", resp.StatusCode, links)
	}
	renameSub := sub + "/devices/" + strconv.FormatInt(info.Devices[2].ID, 10) + "/name"
	if resp, body := h.do(http.MethodPost, renameSub, map[string]string{"name": "Ноут"}, map[string]string{"Sec-Fetch-Site": "same-origin"}); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"name":"Ноут"`) {
		t.Fatalf("rename from the page: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.do(http.MethodPost, renameSub, map[string]string{"name": "x"}, map[string]string{"Sec-Fetch-Site": "cross-site"}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("rename from another site: %d", resp.StatusCode)
	}
	if resp, body := h.do(http.MethodPost, renameSub, map[string]string{"name": strings.Repeat("a", 41)}, map[string]string{"Sec-Fetch-Site": "same-origin"}); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "device_name_long") {
		t.Fatalf("a long name from the page: %d %s", resp.StatusCode, body)
	}

	// Strict mode: an app without an id gets no keys.
	if err := settings.Set(ctx, set, settings.KeyRequireHWID, true); err != nil {
		t.Fatal(err)
	}
	if resp, links := fetch(""); resp.Header.Get("X-Hwid-Not-Supported") != "true" || !strings.Contains(links, "127.0.0.1:1") {
		t.Fatalf("an app without an id in strict mode: %v %q", resp.Header, links)
	}
}
