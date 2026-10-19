package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
)

// The admin of a fresh panel is its owner: the owner alone reaches every section and
// manages the other admins. Without the flag the panel starts with an admin who may sign
// in and nothing else — every section answered "no grant", and the admins' section too.
func TestBootstrapMakesTheFirstAdminTheOwner(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	set := settings.New(st.Q)
	args := []string{"--public-host", "203.0.113.10", "--port", "21355", "--username", "admin", "--password-stdin"}
	pw := strings.NewReader("correct-horse-battery\n")
	var out bytes.Buffer
	if err := bootstrap(ctx, st, set, args, pw, &out); err != nil {
		t.Fatal(err)
	}
	admins, err := st.Q.ListAdmins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(admins) != 1 {
		t.Fatalf("admins: %d", len(admins))
	}
	if admins[0].IsOwner == 0 {
		t.Fatalf("the first admin is not the owner: %+v", admins[0])
	}
	if admins[0].Username != "admin" {
		t.Fatalf("username: %q", admins[0].Username)
	}
}
