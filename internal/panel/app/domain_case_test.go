package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cozy/internal/panel/dnscheck"
)

// The domain is kept as it is written down, but in the one spelling: a DNS name does not
// care about the case, and the ACME server brings the name of an order to the lower one
// (RFC 8555 §7.4) — a domain with capitals in it would be refused for a name the server
// only re-cased. The settings, the certificate and the nodes' SNI must all name the same
// host.
func TestDomainIsKeptInOneSpelling(t *testing.T) {
	doh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ans := map[string]any{"Status": 3, "Answer": []any{}}
		// A real resolver answers for the name whatever its case.
		if strings.EqualFold(r.URL.Query().Get("name"), "vpn.example.com") {
			ans["Status"] = 0
			ans["Answer"] = []any{map[string]any{"type": 1, "data": "203.0.113.10"}}
		}
		_ = json.NewEncoder(w).Encode(ans)
	}))
	t.Cleanup(doh.Close)
	h := newHarness(t, func(o *Options) { o.DNS = &dnscheck.Checker{Resolvers: []string{doh.URL}, HTTP: http.DefaultClient} })
	if resp, _ := h.login(password, ""); resp.StatusCode != http.StatusOK {
		t.Fatal("login")
	}
	csrf := map[string]string{"X-CSRF-Token": h.csrf}
	api := "/" + adminPath + "/api/v1/settings"
	if resp, body := h.do(http.MethodPatch, api, map[string]any{"public_host": "203.0.113.10"}, csrf); resp.StatusCode != http.StatusOK {
		t.Fatalf("public host: %d %s", resp.StatusCode, body)
	}

	// A domain written with capitals is saved in the lower case.
	resp, body := h.do(http.MethodPatch, api, map[string]any{"domain": "VPN.Example.COM"}, csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("domain: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"domain":"vpn.example.com"`) {
		t.Fatalf("the domain was not brought to the lower case: %s", body)
	}

	// And it is the one the panel keeps.
	resp, body = h.do(http.MethodGet, api, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read: %d %s", resp.StatusCode, body)
	}
	var view struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatal(err)
	}
	if view.Domain != "vpn.example.com" {
		t.Fatalf("the panel kept %q", view.Domain)
	}

	// A trailing dot is the same name: it goes away too.
	if resp, body := h.do(http.MethodPatch, api, map[string]any{"domain": "vpn.example.com."}, csrf); resp.StatusCode != http.StatusOK {
		t.Fatalf("domain with a dot: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do(http.MethodGet, api, nil, nil)
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatal(err)
	}
	if view.Domain != "vpn.example.com" {
		t.Fatalf("the trailing dot stayed: %q", view.Domain)
	}

	// The address of the server is not a domain: it keeps its writing.
	if resp, body := h.do(http.MethodPatch, api, map[string]any{"public_host": "203.0.113.10"}, csrf); resp.StatusCode != http.StatusOK {
		t.Fatalf("public host: %d %s", resp.StatusCode, body)
	}
}
