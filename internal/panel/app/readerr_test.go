package app

import (
	"net/http"
	"strings"
	"testing"

	"cozy/internal/panel/tgbot"
)

// A Telegram PATCH is checked whole before anything is saved: a bad token leaves the new
// route unsaved.
func TestTelegramPatchIsAllOrNothing(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.login(password, ""); resp.StatusCode != http.StatusOK {
		t.Fatal("login")
	}
	api := "/" + adminPath + "/api/v1/telegram"
	resp, body := h.do(http.MethodPatch, api, map[string]any{"route": map[string]any{"mode": "proxy", "proxy": "http://127.0.0.1:1"}, "token": "not-a-token"},
		map[string]string{"X-CSRF-Token": h.csrf})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "tg_token_format") {
		t.Fatalf("bad token: %d %s", resp.StatusCode, body)
	}
	if raw, _ := h.st.Q.GetSetting(t.Context(), tgbot.KeyRoute); raw != "" {
		t.Fatalf("route saved despite the refusal: %s", raw)
	}
}
