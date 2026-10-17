package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store/db"
)

// A subscriber may take a new link within the rules; the old one stops working at once.
func TestReissueByOwnerWithinTheRules(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	st, users, ch := setup(t, &now)
	ctx := context.Background()
	clock := func() time.Time { return now }
	clockP := clock
	pool := NewPool(st, clockP)
	users = NewUsers(st, pool, ch, clockP)
	tariffs, _ := st.Q.ListTariffs(ctx)
	u, err := users.Create(ctx, CreateInput{Name: "a", TariffID: tariffs[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	oldToken := u.SubToken
	if _, err := st.Q.GetUserBySubToken(ctx, oldToken); err != nil {
		t.Fatal(err)
	}
	got, err := users.ReissueByOwner(ctx, u.ID, "miniapp")
	if err != nil {
		t.Fatal(err)
	}
	if got.SubToken == oldToken || len(got.SubToken) != 24 {
		t.Fatalf("the token did not change: %q", got.SubToken)
	}
	if _, err := st.Q.GetUserBySubToken(ctx, oldToken); err == nil {
		t.Fatal("the old token still resolves")
	}
	if _, err := st.Q.GetUserBySubToken(ctx, got.SubToken); err != nil {
		t.Fatalf("the new token: %v", err)
	}
	if _, err := users.ReissueByOwner(ctx, u.ID, "bot"); !errors.Is(err, ErrReissueCooldown) {
		t.Fatalf("the second link the same day: %v", err)
	}
	// The admin's reissue is outside the rules.
	if _, err := users.Reissue(ctx, u.ID); err != nil {
		t.Fatalf("admin reissue: %v", err)
	}
	// A day later the subscriber may again.
	now = now.Add(24 * time.Hour)
	third, err := users.ReissueByOwner(ctx, u.ID, "bot")
	if err != nil {
		t.Fatalf("after a day: %v", err)
	}
	if third.SubToken == got.SubToken {
		t.Fatal("the link did not change again")
	}
	// The journal says a subscriber did it, through which door.
	var n int
	if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM audit_log WHERE action = 'user.reissue' AND admin_id IS NULL AND json_extract(details, '$.actor') = 'subscriber'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("subscriber audit rows: %d, want 2", n)
	}
	var via string
	if err := st.DB.QueryRowContext(ctx, "SELECT json_extract(details, '$.via') FROM audit_log WHERE action = 'user.reissue' ORDER BY id LIMIT 1").Scan(&via); err != nil || via != "miniapp" {
		t.Fatalf("the first way: %q %v", via, err)
	}
}

// Rules off, sliding windows, unlimited, and a broken save falling back.
func TestReissueRulesOffAndWindows(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	st, users, ch := setup(t, &now)
	ctx := context.Background()
	clock := func() time.Time { return now }
	pool := NewPool(st, clock)
	users = NewUsers(st, pool, ch, clock)
	set := settings.New(st.Q)
	tariffs, _ := st.Q.ListTariffs(ctx)
	u, _ := users.Create(ctx, CreateInput{Name: "a", TariffID: tariffs[1].ID})

	if err := settings.Set(ctx, set, settings.KeyReissueRules, ReissueRules{Off: true, Limit: 1, Days: 1}); err != nil {
		t.Fatal(err)
	}
	stt, err := users.ReissueState(ctx, u.ID)
	if err != nil || stt.Left() != 0 {
		t.Fatalf("off: %+v %v", stt, err)
	}
	if _, err := users.ReissueByOwner(ctx, u.ID, "bot"); !errors.Is(err, ErrReissueOff) {
		t.Fatalf("reissue when off: %v", err)
	}
	// Two a week: the third waits for the first day of the window.
	if err := settings.Set(ctx, set, settings.KeyReissueRules, ReissueRules{Limit: 2, Days: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := users.ReissueByOwner(ctx, u.ID, "bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := users.ReissueByOwner(ctx, u.ID, "bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := users.ReissueByOwner(ctx, u.ID, "bot"); !errors.Is(err, ErrReissueCooldown) {
		t.Fatalf("third in the window: %v", err)
	}
	now = now.Add(7*24*time.Hour + time.Second)
	if _, err := users.ReissueByOwner(ctx, u.ID, "bot"); err != nil {
		t.Fatalf("after the window: %v", err)
	}
	// Unlimited: never a cooldown.
	if err := settings.Set(ctx, set, settings.KeyReissueRules, ReissueRules{Limit: 0, Days: 1}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := users.ReissueByOwner(ctx, u.ID, "bot"); err != nil {
			t.Fatalf("unlimited link %d: %v", i, err)
		}
	}
	stt, _ = users.ReissueState(ctx, u.ID)
	if stt.Left() != -1 {
		t.Fatalf("unlimited left: %+v", stt)
	}
	if _, err := users.ReissueByOwner(ctx, u.ID+999, "bot"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown user: %v", err)
	}
	// A broken save is not obeyed.
	if _, err := st.DB.ExecContext(ctx, "UPDATE settings SET value = '{broken' WHERE key = 'link_reissue'"); err != nil {
		t.Fatal(err)
	}
	r, err := LoadReissueRules(ctx, st.Q)
	if err != nil || r != DefaultReissueRules {
		t.Fatalf("broken rules: %+v %v", r, err)
	}
}

// History older than any window is pruned; the admin's reissue is not counted.
func TestReissuesArePruned(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	st, _, _ := setup(t, &now)
	ctx := context.Background()
	if _, err := st.DB.ExecContext(ctx, "INSERT INTO users (id, name, sub_token, period_start, created_at, updated_at) VALUES (1, 'a', 'tok', 1, 1, 1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, "INSERT INTO link_reissues (user_id, at) VALUES (1, ?)", now.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := PruneDeviceData(ctx, st.Q, now); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM link_reissues").Scan(&n); err != nil || n != 1 {
		t.Fatalf("fresh rows pruned: %d %v", n, err)
	}
	if err := PruneDeviceData(ctx, st.Q, now.Add(unbindKeepHours*time.Hour+time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM link_reissues").Scan(&n); err != nil || n != 0 {
		t.Fatalf("old rows kept: %d %v", n, err)
	}
	_ = db.LinkReissue{}
}
