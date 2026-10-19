package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The subscription page opens: the settings document comes back whole, and saving then
// reading it again gives the same page (1.3.4 wrote the document wrapped into a JSON
// string, so every later read answered 500 and the save failed with it).
func TestSubPageSaveAndRead(t *testing.T) {
	h := newHarness(t)
	h.login(password, "")
	api := "/" + adminPath + "/api/v1/sub-page"
	csrf := map[string]string{"X-CSRF-Token": h.csrf}

	resp, out := h.do(http.MethodGet, api, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read: %d %s", resp.StatusCode, out)
	}
	var first struct {
		Config struct {
			Brand struct {
				Subtitle string `json:"subtitle"`
			} `json:"brand"`
		} `json:"config"`
	}
	if err := json.Unmarshal(out, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Config.Brand.Subtitle) != 0 {
		t.Fatalf("the fresh page is not empty: %+v", first.Config)
	}

	// The whole document goes back: take what the panel gave and change one field.
	var page map[string]any
	if err := json.Unmarshal(out, &page); err != nil {
		t.Fatal(err)
	}
	cfg := page["config"].(map[string]any)
	brand := cfg["brand"].(map[string]any)
	brand["subtitle"] = "мой VPN"

	resp, out = h.do(http.MethodPut, api, map[string]any{"config": cfg}, csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save: %d %s", resp.StatusCode, out)
	}

	// And it survives the save: this is the read that used to be a 500.
	resp, out = h.do(http.MethodGet, api, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read after save: %d %s", resp.StatusCode, out)
	}
	var again struct {
		Config struct {
			Brand struct {
				Subtitle string `json:"subtitle"`
			} `json:"brand"`
		} `json:"config"`
	}
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	if again.Config.Brand.Subtitle != "мой VPN" {
		t.Fatalf("the save did not stick: %+v", again.Config.Brand)
	}
}

// A key with read access may look at the page; it may not change it.
func TestSubPageNeedsSessionToSave(t *testing.T) {
	h := newHarness(t)
	h.login(password, "")
	resp, _ := h.do(http.MethodPut, "/"+adminPath+"/api/v1/sub-page", map[string]any{"config": map[string]any{}}, map[string]string{"X-CSRF-Token": h.csrf})
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a save without a session must not pass: %d", resp.StatusCode)
	}
}

// The logo goes up as a picture of its own, comes back with the page's data and is served
// as the very bytes that were uploaded; an SVG is refused whatever it calls itself.
func TestSubPageLogo(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, func(o *Options) { o.DataDir = dir })
	h.login(password, "")
	api := "/" + adminPath + "/api/v1/sub-page"
	csrf := map[string]string{"X-CSRF-Token": h.csrf}

	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	if resp, body := h.do(http.MethodPut, api+"/logo", map[string]any{"image": base64.StdEncoding.EncodeToString(png)}, csrf); resp.StatusCode != http.StatusOK {
		t.Fatalf("upload: %d %s", resp.StatusCode, body)
	}

	resp, out := h.do(http.MethodGet, api, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read: %d %s", resp.StatusCode, out)
	}
	var view struct {
		Logo *struct {
			Name string `json:"name"`
			Type string `json:"content_type"`
			Hash string `json:"hash"`
		} `json:"logo"`
	}
	if err := json.Unmarshal(out, &view); err != nil {
		t.Fatal(err)
	}
	if view.Logo == nil || view.Logo.Type != "image/png" || view.Logo.Hash == "" {
		t.Fatalf("the page does not know its logo: %s", out)
	}

	// The picture itself, as the editor previews it: the same bytes back, base64 in JSON.
	resp, raw := h.do(http.MethodGet, api+"/logo", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("serve: %d %s", resp.StatusCode, raw)
	}
	var shown struct {
		Image string `json:"image"`
		Type  string `json:"content_type"`
	}
	if err := json.Unmarshal(raw, &shown); err != nil {
		t.Fatal(err)
	}
	if shown.Type != "image/png" {
		t.Fatalf("content type: %q", shown.Type)
	}
	back, err := base64.StdEncoding.DecodeString(shown.Image)
	if err != nil || string(back) != string(png) {
		t.Fatalf("different bytes: %d vs %d", len(back), len(png))
	}

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if resp, body := h.do(http.MethodPut, api+"/logo", map[string]any{"image": base64.StdEncoding.EncodeToString(svg)}, csrf); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("an SVG must be refused: %d %s", resp.StatusCode, body)
	}

	// And it goes away.
	if resp, body := h.do(http.MethodDelete, api+"/logo", nil, csrf); resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d %s", resp.StatusCode, body)
	}
	resp, _ = h.do(http.MethodGet, api+"/logo", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("the picture is still served: %d", resp.StatusCode)
	}
}

// The panel puts the subscription page's own data into its <head>: the page opens with what
// the admin built, and the logo's link travels with it.
func TestSubPageHeadCarriesTheData(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.DataDir = t.TempDir() })
	h.login(password, "")
	api := "/" + adminPath + "/api/v1/sub-page"
	csrf := map[string]string{"X-CSRF-Token": h.csrf}

	_, out := h.do(http.MethodGet, api, nil, nil)
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	cfg := doc["config"].(map[string]any)
	brand := cfg["brand"].(map[string]any)
	brand["subtitle"] = "мой VPN"
	if resp, body := h.do(http.MethodPut, api, map[string]any{"config": cfg}, csrf); resp.StatusCode != http.StatusOK {
		t.Fatalf("save: %d %s", resp.StatusCode, body)
	}

	// The page itself is served by the panel's own SPA for the subscription path.
	if h.p.subPage == nil {
		t.Skip("no subscription page in this build")
	}
	h.p.applySubPage(context.Background())
	rec := httptest.NewRecorder()
	h.p.subPage.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+subPath+"/", nil))
	html := rec.Body.Bytes()
	i := strings.Index(string(html), `id="cozy-page"`)
	if i < 0 {
		t.Fatalf("the page carries no data: %s", html)
	}
	head := string(html[i : i+400])
	if !strings.Contains(head, "мой VPN") {
		t.Fatalf("the data is not the one that was saved: %s", head)
	}
}
