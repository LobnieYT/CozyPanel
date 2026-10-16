package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cozy/internal/panel/store/db"
)

func TestCleanDeviceName(t *testing.T) {
	for _, s := range []string{"", "  "} {
		if got, err := CleanDeviceName(s); got != "" || err != nil {
			t.Fatalf("%q: %q %v, want empty", s, got, err)
		}
	}
	if got, err := CleanDeviceName("  Мамин телефон  "); got != "Мамин телефон" || err != nil {
		t.Fatalf("trim: %q %v", got, err)
	}
	if got, err := CleanDeviceName(strings.Repeat("a", 40)); got == "" || err != nil {
		t.Fatalf("40 runes: %q %v", got, err)
	}
	if _, err := CleanDeviceName(strings.Repeat("a", 41)); !isFieldErr(err, "name", "device_name_long") {
		t.Fatalf("41 runes: %v", err)
	}
	for _, s := range []string{"a\tb", "a\nb", "a\u200eb"} {
		if _, err := CleanDeviceName(s); !isFieldErr(err, "name", "name_chars") {
			t.Fatalf("%q: %v, want name_chars", s, err)
		}
	}
}

func isFieldErr(err error, field, code string) bool {
	var fe *FieldError
	return errors.As(err, &fe) && fe.Field == field && fe.Code == code
}

// A bound device's own name survives rebinds; a banned id stays out of the user.
func TestRenameAndBanDevice(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	st, users, ch := setup(t, &now)
	ctx := context.Background()
	clock := func() time.Time { return now }
	devs := NewDevices(st, NewPool(st, clock), ch, clock)
	tariffs, _ := st.Q.ListTariffs(ctx)
	u, err := users.Create(ctx, CreateInput{Name: "a", TariffID: tariffs[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	other, err := users.Create(ctx, CreateInput{Name: "b", TariffID: tariffs[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	phone := DeviceInfo{HWID: "phone-0123456789", OS: "Android", OSVersion: "15", Model: "Pixel 9", App: "Happ/3.4", IP: "203.0.113.5"}
	s1, err := devs.Bind(ctx, u, phone, false)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := st.Q.ListBoundDevices(ctx, u.ID)
	id := list[0].ID

	saved, err := devs.Rename(ctx, u.ID, id, "  Мамин телефон  ")
	if err != nil || saved != "Мамин телефон" {
		t.Fatalf("rename: %q %v", saved, err)
	}
	if _, err := devs.Bind(ctx, u, DeviceInfo{HWID: phone.HWID, Model: "Pixel 10", App: "Happ/3.5"}, false); err != nil {
		t.Fatal(err)
	}
	dev, _ := st.Q.GetBoundDevice(ctx, db.GetBoundDeviceParams{UserID: u.ID, Hwid: phone.HWID})
	if dev.Name != "Мамин телефон" || dev.SlotID != s1.ID || dev.Model != "Pixel 10" {
		t.Fatalf("the name after a rebind: %+v", dev)
	}
	if _, err := devs.Rename(ctx, other.ID, id, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign rename: %v", err)
	}
	if _, err := devs.Rename(ctx, u.ID, id, strings.Repeat("a", 41)); !isFieldErr(err, "name", "device_name_long") {
		t.Fatalf("long rename: %v", err)
	}
	if saved, err := devs.Rename(ctx, u.ID, id, ""); err != nil || saved != "" {
		t.Fatalf("clear: %q %v", saved, err)
	}
	dev, _ = st.Q.GetBoundDevice(ctx, db.GetBoundDeviceParams{UserID: u.ID, Hwid: phone.HWID})
	if DeviceLabel(dev) != "Pixel 10" {
		t.Fatalf("the label after clearing: %q", DeviceLabel(dev))
	}

	admin, err := st.Q.CreateAdmin(ctx, db.CreateAdminParams{Username: "root", PasswordHash: "x", CreatedAt: now.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := devs.Ban(ctx, other.ID, id, admin.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign ban: %v", err)
	}
	shared, err := devs.Bind(ctx, u, DeviceInfo{App: "clash"}, false)
	if err != nil {
		t.Fatal(err)
	}
	sharedList, _ := st.Q.ListBoundDevices(ctx, u.ID)
	var sharedID int64
	for _, d := range sharedList {
		if d.Hwid == "" {
			sharedID = d.ID
		}
	}
	if sharedID == 0 {
		t.Fatalf("no shared place among %+v", sharedList)
	}
	_ = shared
	if _, err := devs.Ban(ctx, u.ID, sharedID, admin.ID); !errors.Is(err, ErrBanShared) {
		t.Fatalf("banning the shared place: %v", err)
	}

	before, _ := st.Q.CountBoundDevices(ctx, u.ID)
	ban, err := devs.Ban(ctx, u.ID, id, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ban.Hwid != phone.HWID || ban.Label != "Pixel 10" || !ban.AdminID.Valid || ban.AdminID.Int64 != admin.ID {
		t.Fatalf("the ban row: %+v", ban)
	}
	if s, _ := st.Q.GetSlot(ctx, s1.ID); s.State != "burned" {
		t.Fatalf("the banned device's keys must burn: %s", s.State)
	}
	if n, _ := st.Q.CountBoundDevices(ctx, u.ID); n != before-1 {
		t.Fatalf("bound after the ban: %d", n)
	}
	banned, err := devs.Banned(ctx, u.ID, phone.HWID)
	if err != nil || !banned {
		t.Fatalf("banned: %v %v", banned, err)
	}
	if _, err := devs.Bind(ctx, u, phone, false); !errors.Is(err, ErrDeviceBanned) {
		t.Fatalf("a banned id binds again: %v", err)
	}
	if n, _ := st.Q.CountBoundDevices(ctx, u.ID); n != before-1 {
		t.Fatalf("the banned bind took a place: %d", n)
	}
	// The same id on another user is not affected.
	if _, err := devs.Bind(ctx, other, phone, false); err != nil {
		t.Fatalf("the same id on another user: %v", err)
	}
	bans, err := st.Q.ListDeviceBans(ctx, u.ID)
	if err != nil || len(bans) != 1 || bans[0].Label != "Pixel 10" || bans[0].AdminName != "root" {
		t.Fatalf("bans: %+v %v", bans, err)
	}
	// Reissue burns everything but does not lift the ban.
	if _, err := users.Reissue(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := devs.Bind(ctx, u, phone, false); !errors.Is(err, ErrDeviceBanned) {
		t.Fatalf("reissue lifted the ban: %v", err)
	}
	if _, err := devs.Unban(ctx, other.ID, ban.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign unban: %v", err)
	}
	if _, err := devs.Unban(ctx, u.ID, ban.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second unban: %v", err)
	}
	if _, err := devs.Unban(ctx, u.ID, ban.ID); err != nil {
		t.Fatal(err)
	}
	if banned, _ := devs.Banned(ctx, u.ID, phone.HWID); banned {
		t.Fatal("still banned after the unban")
	}
	if _, err := devs.Bind(ctx, u, phone, false); err != nil {
		t.Fatalf("bind after the unban: %v", err)
	}
}
