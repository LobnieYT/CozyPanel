package subs

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Bundled subscription assets (app icons under sub/) must reach the page handler
// like logo.png does: otherwise the page shows letter placeholders with no error.
func TestSubAssetsReachPage(t *testing.T) {
	var got []string
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})
	h := &Handler{page: page}
	// App icons under sub/apps reach the page like logo.png does; anything else
	// keeps the old answers.
	for _, p := range []string{"/sub/apps/happ.png", "/logo.png", "/assets/app-abc.js"} {
		got = nil
		r := httptest.NewRequest(http.MethodGet, p, nil)
		h.ServeHTTP(httptest.NewRecorder(), r)
		if len(got) != 1 || got[0] != p {
			t.Errorf("GET %s: page got %v", p, got)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/sub/apps", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /sub/apps: status %d, want 404", w.Code)
	}
}
