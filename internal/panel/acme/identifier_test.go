package acme

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
	"cozy/internal/panel/tlscert"
)

// The name the order is made for is the canonical one: the ACME server brings the name of
// an order to the lower case (RFC 8555 §7.4), and the client library compares the
// identifiers letter by letter — a domain with capitals would be refused for a name the
// server only re-cased.
func TestIdentifierIsCanonical(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set := settings.New(st.Q)
	if err := settings.Set(ctx, set, settings.KeyDomain, "Alpha-Sun-X2uTG1QW.expresshost.cloud"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	fallback, err := tlscert.LoadOrCreateSelfSigned(t.TempDir(), "vpn.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	holder := &tlscert.Holder{}
	holder.Set(fallback)
	m := New(t.TempDir(), holder, fallback, set, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)

	id, err := m.identifier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id != "alpha-sun-x2utg1qw.expresshost.cloud" {
		t.Fatalf("the name is not canonical: %q", id)
	}
	// What the server would answer with is the same name: nothing to "change".
	if settings.CanonicalDomain(id) != id {
		t.Fatalf("the server would change the name: %q", id)
	}
}

// Without a domain the address is the name, and it stays as it is.
func TestIdentifierFallsBackToTheAddress(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set := settings.New(st.Q)
	if err := settings.Set(ctx, set, settings.KeyPublicHost, "77.238.243.225"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	fallback, err := tlscert.LoadOrCreateSelfSigned(t.TempDir(), "77.238.243.225", now)
	if err != nil {
		t.Fatal(err)
	}
	holder := &tlscert.Holder{}
	holder.Set(fallback)
	m := New(t.TempDir(), holder, fallback, set, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)

	id, err := m.identifier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id != "77.238.243.225" {
		t.Fatalf("the address is not the name: %q", id)
	}
}
