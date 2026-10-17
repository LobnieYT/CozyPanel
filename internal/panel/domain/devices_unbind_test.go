package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store/db"
)

func TestUnbindRules(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	st, users, ch := setup(t, &now)
	ctx := context.Background()
	clock := func() time.Time { return now }
	devs := NewDevices(st, NewPool(st, clock), ch, clock)
	set := settings.New(st.Q)
	if err := settings.Set(ctx, set, settings.KeyUnbindRules, UnbindRules{Limit: 2, Days: 7, ReturnHours: 6}); err != nil {
		t.Fatal(err)
	}
	tariffs, _ := st.Q.ListTariffs(ctx)
	u, err := users.Create(ctx, CreateInput{Name: "a", TariffID: tariffs[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(hwid string) int64 {
		t.Helper()
		s, err := devs.Bind(ctx, u, DeviceInfo{HWID: hwid}, false)
		if err != nil {
			t.Fatalf("bind %s: %v", hwid, err)
		}
		_ = s
		list, _ := st.Q.ListBoundDevices(ctx, u.ID)
		for _, d := range list {
			if d.Hwid == hwid {
				return d.ID
			}
		}
		t.Fatalf("no device %s", hwid)
		return 0
	}
	mk("phone-0123456789")
	mk("laptop-0123456789")
	third, err := devs.Bind(ctx, u, DeviceInfo{HWID: "tablet-0123456789"}, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = third
	list, _ := st.Q.ListBoundDevices(ctx, u.ID)
	if err := devs.Unbind(ctx, u.ID, list[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := devs.Unbind(ctx, u.ID, list[1].ID, true); err != nil {
		t.Fatal(err)
	}
	stt, err := UnbindStateOf(ctx, st.Q, u.ID, now)
	if err != nil || stt.Used != 2 || stt.Left() != 0 {
		t.Fatalf("state: %+v %v", stt, err)
	}
	if !stt.Next.Equal(now.Add(7 * 24 * time.Hour).UTC()) {
		t.Fatalf("next: %s", stt.Next)
	}
	list, _ = st.Q.ListBoundDevices(ctx, u.ID)
	list, _ = st.Q.ListBoundDevices(ctx, u.ID)
	if err := devs.Unbind(ctx, u.ID, list[0].ID, true); !errors.Is(err, ErrUnbindCooldown) {
		t.Fatalf("third unbind in the window: %v", err)
	}
	// The admin is outside the rules.
	if err := devs.Unbind(ctx, u.ID, list[0].ID, false); err != nil {
		t.Fatalf("admin unbind: %v", err)
	}
	// An unbound device waits out the pause: it may not bind again yet.
	var ue *UnboundError
	_, err = devs.Bind(ctx, u, DeviceInfo{HWID: "phone-0123456789"}, false)
	if !errors.As(err, &ue) || !ue.Until.Equal(now.Add(6*time.Hour).UTC()) {
		t.Fatalf("paused bind: %v %+v", err, ue)
	}
	if banned, _ := devs.Banned(ctx, u.ID, "phone-0123456789"); banned {
		t.Fatal("a pause is not an admin ban")
	}
	// The admin's ban is forever: it reports banned, not paused.
	if _, err := devs.Bind(ctx, u, DeviceInfo{HWID: "spare-0123456789"}, false); err != nil {
		t.Fatalf("bind a spare: %v", err)
	}
	banList, _ := st.Q.ListBoundDevices(ctx, u.ID)
	var banID int64
	for _, d := range banList {
		if d.Hwid == "spare-0123456789" {
			banID = d.ID
		}
	}
	if banID == 0 {
		t.Fatal("no spare device to ban")
	}
	ban, err := devs.Ban(ctx, u.ID, banID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if banned, _ := devs.Banned(ctx, u.ID, ban.Hwid); !banned {
		t.Fatal("the admin ban must report banned")
	}
	if _, err := devs.Bind(ctx, u, DeviceInfo{HWID: ban.Hwid}, false); !errors.Is(err, ErrDeviceBanned) {
		t.Fatalf("banned bind: %v", err)
	}
	// The pause runs out: the device binds again, old history is pruned.
	now = now.Add(7 * time.Hour)
	if _, err := devs.Bind(ctx, u, DeviceInfo{HWID: "phone-0123456789"}, false); err != nil {
		t.Fatalf("bind after the pause: %v", err)
	}
	now = now.Add(400 * 24 * time.Hour)
	if err := PruneDeviceData(ctx, st.Q, now); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM device_unbinds").Scan(&n); err != nil || n != 0 {
		t.Fatalf("unbind history after prune: %d %v", n, err)
	}
	if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM device_bans").Scan(&n); err != nil || n != 1 {
		t.Fatalf("bans after prune: %d %v (the admin ban stays)", n, err)
	}
	// No limit: the state stays open.
	if err := settings.Set(ctx, set, settings.KeyUnbindRules, UnbindRules{Limit: 0, Days: 7, ReturnHours: 0}); err != nil {
		t.Fatal(err)
	}
	stt, _ = UnbindStateOf(ctx, st.Q, u.ID, now)
	if stt.Left() != -1 || !stt.Next.IsZero() {
		t.Fatalf("unlimited: %+v", stt)
	}
}

// Off means the subscriber never unbinds; ReturnNever waits for the admin.
func TestUnbindOffAndNever(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	st, users, ch := setup(t, &now)
	ctx := context.Background()
	clock := func() time.Time { return now }
	devs := NewDevices(st, NewPool(st, clock), ch, clock)
	set := settings.New(st.Q)
	if err := settings.Set(ctx, set, settings.KeyUnbindRules, UnbindRules{Off: true, Limit: 1, Days: 1}); err != nil {
		t.Fatal(err)
	}
	tariffs, _ := st.Q.ListTariffs(ctx)
	u, _ := users.Create(ctx, CreateInput{Name: "a", TariffID: tariffs[1].ID})
	if _, err := devs.Bind(ctx, u, DeviceInfo{HWID: "phone-0123456789"}, false); err != nil {
		t.Fatal(err)
	}
	list, _ := st.Q.ListBoundDevices(ctx, u.ID)
	stt, _ := UnbindStateOf(ctx, st.Q, u.ID, now)
	if stt.Left() != 0 {
		t.Fatalf("off: %+v", stt)
	}
	if err := devs.Unbind(ctx, u.ID, list[0].ID, true); !errors.Is(err, ErrUnbindOff) {
		t.Fatalf("subscriber unbind when off: %v", err)
	}
	if err := devs.Unbind(ctx, u.ID, list[0].ID, false); err != nil {
		t.Fatalf("admin unbind when off: %v", err)
	}
	if err := settings.Set(ctx, set, settings.KeyUnbindRules, UnbindRules{Limit: 1, Days: 1, ReturnHours: ReturnNever}); err != nil {
		t.Fatal(err)
	}
	if _, err := devs.Bind(ctx, u, DeviceInfo{HWID: "tablet-0123456789"}, false); err != nil {
		t.Fatal(err)
	}
	list, _ = st.Q.ListBoundDevices(ctx, u.ID)
	if err := devs.Unbind(ctx, u.ID, list[0].ID, true); err != nil {
		t.Fatal(err)
	}
	var ue *UnboundError
	_, err := devs.Bind(ctx, u, DeviceInfo{HWID: "tablet-0123456789"}, false)
	if !errors.As(err, &ue) || !Forever(ue.Until) {
		t.Fatalf("never bind: %v %+v", err, ue)
	}
}

// Online mode counts addresses on the node, not bound places: binding stays open,
// the tariff's mode is inherited and the user's own choice wins.
func TestDeviceModeOnlineDoesNotCapBinding(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	st, users, ch := setup(t, &now)
	ctx := context.Background()
	clock := func() time.Time { return now }
	devs := NewDevices(st, NewPool(st, clock), ch, clock)
	tariffs, _ := st.Q.ListTariffs(ctx)
	std := tariffs[1] // device limit 3
	if err := st.Q.SetTariffDeviceMode(ctx, db.SetTariffDeviceModeParams{DeviceMode: DeviceModeOnline, ID: std.ID}); err != nil {
		t.Fatal(err)
	}
	u, err := users.Create(ctx, CreateInput{Name: "a", TariffID: std.ID})
	if err != nil {
		t.Fatal(err)
	}
	if u.DeviceMode != DeviceModeOnline {
		t.Fatalf("the user did not take the tariff mode: %q", u.DeviceMode)
	}
	for _, hwid := range []string{"a-0123456789", "b-0123456789", "c-0123456789", "d-0123456789", "e-0123456789"} {
		if _, err := devs.Bind(ctx, u, DeviceInfo{HWID: hwid}, false); err != nil {
			t.Fatalf("bind %s in online mode: %v", hwid, err)
		}
	}
	if n, _ := st.Q.CountBoundDevices(ctx, u.ID); n != 5 {
		t.Fatalf("bound in online mode: %d", n)
	}
	if _, err := users.Update(ctx, u.ID, Patch{DeviceMode: &[]string{DeviceModeBound}[0]}); err != nil {
		t.Fatal(err)
	}
	u, _ = st.Q.GetUser(ctx, u.ID)
	if u.DeviceMode != DeviceModeBound {
		t.Fatalf("the patch did not win: %q", u.DeviceMode)
	}
	if _, err := users.Update(ctx, u.ID, Patch{DeviceMode: &[]string{"sideways"}[0]}); !isFieldErr(err, "device_mode", "bad_device_mode") {
		t.Fatalf("junk mode: %v", err)
	}
}
