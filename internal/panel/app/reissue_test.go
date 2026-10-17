package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"cozy/internal/panel/store/db"
	"cozy/internal/panel/tgbot"

	"cozy/internal/panel/domain"
	"cozy/internal/panel/settings"
)

// The Mini App gives a new link only to the account that owns it, only with Telegram's
// proof, and only within the admin's rules.
func TestMiniAppReissue(t *testing.T) {
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
	pool := domain.NewPool(h.st, clock)
	users := domain.NewUsers(h.st, pool, noChanges{}, clock)
	tariffs, _ := h.st.Q.ListTariffs(ctx)
	u, err := users.Create(ctx, domain.CreateInput{Name: "Анна", TariffID: tariffs[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	other, err := users.Create(ctx, domain.CreateInput{Name: "Борис", TariffID: tariffs[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	// The bot's Mini App: account 555 owns u's subscription.
	tgToken := "123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw1"
	if err := settings.Set(ctx, set, tgbot.KeyEnabled, true); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, set, tgbot.KeyToken, tgToken); err != nil {
		t.Fatal(err)
	}
	if err := h.st.Q.LinkTg(ctx, db.LinkTgParams{UserID: u.ID, TgID: 555, CreatedAt: h.now.Unix()}); err != nil {
		t.Fatal(err)
	}
	sub := "/" + subPath + "/"
	proof := initData(tgToken, 555, h.now)
	post := func(token, proofData string, hdr map[string]string) (*http.Response, []byte) {
		if hdr == nil {
			hdr = map[string]string{"Sec-Fetch-Site": "same-origin"}
		}
		body := map[string]string{"init_data": proofData, "token": token}
		if token == "" {
			body = map[string]string{"init_data": proofData}
		}
		return h.do(http.MethodPost, sub+"tg/reissue", body, hdr)
	}
	type reissueOut struct {
		Token string `json:"token"`
		Code  string `json:"code"`
		After string `json:"reissue_after"`
	}
	// No token: refused.
	if resp, body := post("", proof, nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("no token: %d %s", resp.StatusCode, body)
	}
	// Another account's subscription: refused.
	if resp, body := post(other.SubToken, proof, nil); resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "not_yours") {
		t.Fatalf("a foreign subscription: %d %s", resp.StatusCode, body)
	}
	// Another site: refused.
	if resp, _ := post(u.SubToken, proof, map[string]string{"Sec-Fetch-Site": "cross-site"}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-site: %d", resp.StatusCode)
	}
	// No Telegram proof: refused.
	if resp, body := post(u.SubToken, "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no proof: %d %s", resp.StatusCode, body)
	}
	// The link is unchanged after the refusals.
	if got, _ := h.st.Q.GetUser(ctx, u.ID); got.SubToken != u.SubToken {
		t.Fatal("a refused request changed the link")
	}
	// The page says the button is there.
	resp, body := h.do(http.MethodGet, sub+u.SubToken+"/info", nil, nil)
	var info struct {
		Reissue      bool   `json:"reissue"`
		ReissueOff   bool   `json:"reissue_off"`
		ReissueLimit int    `json:"reissue_limit"`
		ReissueDays  int    `json:"reissue_days"`
		ReissueAfter string `json:"reissue_after"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &info) != nil || !info.Reissue || info.ReissueLimit != 1 || info.ReissueDays != 1 || info.ReissueAfter != "" {
		t.Fatalf("info before: %d %s", resp.StatusCode, body)
	}
	// The owner gets a new link.
	resp, body = post(u.SubToken, proof, nil)
	var out reissueOut
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &out) != nil || len(out.Token) != 24 {
		t.Fatalf("issue: %d %s", resp.StatusCode, body)
	}
	if out.Token == u.SubToken {
		t.Fatal("the token did not change")
	}
	if resp, _ := h.do(http.MethodGet, sub+u.SubToken+"/info", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("the old link still works: %d", resp.StatusCode)
	}
	// The limit is spent.
	resp, body = h.do(http.MethodGet, sub+out.Token+"/info", nil, nil)
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &info) != nil || info.ReissueAfter == "" {
		t.Fatalf("info after: %d %s", resp.StatusCode, body)
	}
	resp, body = post(out.Token, proof, nil)
	if resp.StatusCode != http.StatusTooManyRequests || json.Unmarshal(body, &out) != nil || out.Code != "reissue_cooldown" || out.After == "" {
		t.Fatalf("cooldown: %d %s", resp.StatusCode, body)
	}
	// Rules off: refused, and the page says so.
	if err := settings.Set(ctx, set, settings.KeyReissueRules, domain.ReissueRules{Off: true, Limit: 1, Days: 1}); err != nil {
		t.Fatal(err)
	}
	resp, body = post(out.Token, proof, nil)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "reissue_off") {
		t.Fatalf("off: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(http.MethodGet, sub+out.Token+"/info", nil, nil)
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &info) != nil || !info.ReissueOff {
		t.Fatalf("info off: %d %s", resp.StatusCode, body)
	}
}
