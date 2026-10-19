package acme

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
	"cozy/internal/panel/tlscert"
)

// A real ACME server brings the name of an order to the lower case (RFC 8555 §7.4): a
// domain with capitals in it comes back re-cased, and the client library, which compares
// the identifiers letter by letter, refuses the order for a name it did not change. The
// panel sends the canonical name, so the server has nothing to change — the order goes
// through.
func TestOrderSurvivesTheServerReCasingTheName(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var asked string
	dir := directory(t, func(w http.ResponseWriter, r *http.Request) {
		nonce := "nonce-1"
		w.Header().Set("Replay-Nonce", nonce)
		t.Logf("request: %s %s", r.Method, r.URL.Path)
		switch {
		case r.URL.Path == "/directory":
			// The URLs are absolute: the client takes them as they are.
			base := "https://" + r.Host
			_ = json.NewEncoder(w).Encode(map[string]any{
				"newNonce":   base + "/new-nonce",
				"newAccount": base + "/new-account",
				"newOrder":   base + "/new-order",
			})
		case r.URL.Path == "/new-nonce":
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/new-account":
			w.Header().Set("Location", r.URL.Path+"/acct/1")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "valid"})
		case r.URL.Path == "/new-order":
			// The request is a JWS: the order itself is the payload, base64url.
			body, _ := io.ReadAll(r.Body)
			var jws struct {
				Payload string `json:"payload"`
			}
			_ = json.Unmarshal(body, &jws)
			payload, err := base64.RawURLEncoding.DecodeString(jws.Payload)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var order struct {
				Identifiers []struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"identifiers"`
			}
			_ = json.Unmarshal(payload, &order)
			mu.Lock()
			if len(order.Identifiers) > 0 {
				asked = order.Identifiers[0].Value
			}
			mu.Unlock()
			// The server answers with the name in the lower case, as the real ones do.
			out := map[string]any{
				"status":         "pending",
				"expires":        time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
				"identifiers":    []any{map[string]any{"type": "dns", "value": strings.ToLower(asked)}},
				"authorizations": []any{},
				"finalize":       r.URL.Path + "/finalize/1",
			}
			w.Header().Set("Location", r.URL.Path+"/order/1")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(out)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	// The domain is written with capitals, as it was in the settings.
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set := settings.New(st.Q)
	if err := settings.Set(ctx, set, settings.KeyDomain, "Alpha-Sun-X2uTG1QW.expresshost.cloud"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COZY_ACME_DIRECTORY", dir)
	now := time.Now()
	fallback, err := tlscert.LoadOrCreateSelfSigned(t.TempDir(), "vpn.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	holder := &tlscert.Holder{}
	holder.Set(fallback)
	m := New(t.TempDir(), holder, fallback, set, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)

	// The order goes out; the server re-cases the name back, and the order must not be
	// refused for it.
	m.ensure(ctx)
	mu.Lock()
	askedName := asked
	mu.Unlock()
	if askedName == "" {
		t.Fatalf("the order was never made: %+v", m.Status())
	}
	if askedName != "alpha-sun-x2utg1qw.expresshost.cloud" {
		t.Fatalf("the order asked for %q", askedName)
	}
	if s := m.Status(); strings.Contains(s.Error, "identifiers have been modified") {
		t.Fatalf("the server's re-casing refused the order: %s", s.Error)
	}
}

// An order that comes back with nothing and without an error (the library may answer
// that way when the order failed and there was no authorization to name it by) must not
// take the panel down: it says the order failed.
func TestOrderWithoutACertificateDoesNotFallOver(t *testing.T) {
	ctx := context.Background()
	dir := directory(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Replay-Nonce", "nonce-1")
		switch {
		case r.URL.Path == "/directory":
			base := "https://" + r.Host
			_ = json.NewEncoder(w).Encode(map[string]any{
				"newNonce":   base + "/new-nonce",
				"newAccount": base + "/new-account",
				"newOrder":   base + "/new-order",
			})
		case r.URL.Path == "/new-nonce":
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/new-account":
			w.Header().Set("Location", r.URL.Path+"/acct/1")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "valid"})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set := settings.New(st.Q)
	if err := settings.Set(ctx, set, settings.KeyDomain, "vpn.example.com"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COZY_ACME_DIRECTORY", dir)
	now := time.Now()
	fallback, err := tlscert.LoadOrCreateSelfSigned(t.TempDir(), "vpn.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	holder := &tlscert.Holder{}
	holder.Set(fallback)
	m := New(t.TempDir(), holder, fallback, set, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)

	failed := m.ensure(ctx)
	if !failed {
		t.Fatal("the failed order is not a failure")
	}
	if s := m.Status(); s.Error == "" {
		t.Fatalf("the failure left no mark: %+v", s)
	}
}
