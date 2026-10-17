package acme

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
)

// Only Let's Encrypt certifies an IP address (the short-lived profile), so an IP always
// goes there whatever the admin chose.
func TestEffectiveCA(t *testing.T) {
	for _, c := range []struct {
		chosen, id, want string
	}{
		{CALetsEncrypt, "vpn.example.com", CALetsEncrypt},
		{CAZeroSSL, "vpn.example.com", CAZeroSSL},
		{CAGoogle, "vpn.example.com", CAGoogle},
		{CAZeroSSL, "203.0.113.10", CALetsEncrypt},
		{CAGoogle, "203.0.113.10", CALetsEncrypt},
		{"", "vpn.example.com", CALetsEncrypt},
		{"nonsense", "vpn.example.com", CALetsEncrypt},
	} {
		if got := EffectiveCA(c.chosen, c.id); got != c.want {
			t.Errorf("chosen %q id %q: %q, want %q", c.chosen, c.id, got, c.want)
		}
	}
	if !ValidCA(CAGoogle) || ValidCA("nonsense") {
		t.Error("ValidCA")
	}
	for _, ca := range CAs() {
		if Directory(ca) == "" {
			t.Errorf("no directory for %q", ca)
		}
	}
}

// The chosen CA is Let's Encrypt until the admin picks another.
func TestChosenCA(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	set := settings.New(st.Q)
	ca, err := ChosenCA(ctx, set)
	if err != nil || ca != CALetsEncrypt {
		t.Fatalf("unset: %q %v", ca, err)
	}
	if err := settings.Set(ctx, set, settings.KeyACMECA, CAZeroSSL); err != nil {
		t.Fatal(err)
	}
	if ca, _ := ChosenCA(ctx, set); ca != CAZeroSSL {
		t.Fatalf("chosen: %q", ca)
	}
	// A value nobody knows falls back, it does not break the panel.
	if err := settings.Set(ctx, set, settings.KeyACMECA, "nonsense"); err != nil {
		t.Fatal(err)
	}
	if ca, _ := ChosenCA(ctx, set); ca != CALetsEncrypt {
		t.Fatalf("broken value: %q", ca)
	}
}

func TestEABValidation(t *testing.T) {
	if ValidEABKey("short") {
		t.Error("a short HMAC key must be refused")
	}
	if !ValidEABKey("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=") {
		t.Error("a base64url HMAC key must pass")
	}
	if ValidEABKID("") || ValidEABKID("has space") || !ValidEABKID("abc123") {
		t.Error("the key id")
	}
	if !ValidEmail("a@example.com") || ValidEmail("Name <a@example.com>") || ValidEmail("nonsense") {
		t.Error("the e-mail")
	}
}

// ZeroSSL hands out EAB credentials for an e-mail; a refused answer is a code the panel
// translates.
func TestZeroSSLEAB(t *testing.T) {
	ctx := context.Background()
	old := zeroSSLAPI
	defer func() { zeroSSLAPI = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.PostForm.Get("email") == "" {
			t.Errorf("the request: %v %v", err, r.PostForm)
		}
		if r.FormValue("email") == "refused@example.com" {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]any{"code": 2801, "type": "invalid_email"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "eab_kid": "kid1", "eab_hmac_key": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="})
	}))
	defer srv.Close()
	zeroSSLAPI = srv.URL
	c := srv.Client()

	eab, err := ZeroSSLEAB(ctx, c, "a@example.com")
	if err != nil || eab.KID != "kid1" || !strings.HasPrefix(eab.HMAC, "MDEy") {
		t.Fatalf("the credentials: %+v %v", eab, err)
	}
	if _, err := ZeroSSLEAB(ctx, c, "refused@example.com"); err == nil || !strings.Contains(err.Error(), "zerossl_eab_failed") {
		t.Fatalf("a refused answer: %v", err)
	}
	if _, err := ZeroSSLEAB(ctx, c, ""); err == nil || !strings.Contains(err.Error(), "zerossl_email_required") {
		t.Fatalf("no e-mail: %v", err)
	}
	if _, err := ZeroSSLEAB(ctx, c, "not an address"); err == nil || !strings.Contains(err.Error(), "zerossl_email_required") {
		t.Fatalf("a bad address: %v", err)
	}
}
