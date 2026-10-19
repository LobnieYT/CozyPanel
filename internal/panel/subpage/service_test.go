package subpage

import (
	"context"
	"encoding/json"
	"testing"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
)

// A saved page reads back as itself: the document goes into the setting as an object, not as
// a JSON string wrapped into one (1.3.4 wrote the string, and every read after it was a 500).
func TestSaveReadsBack(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewService(st.Q)

	want := Default()
	want.Brand.Subtitle = "Мой VPN"
	want.CSS = "body{color:red}"
	if ps, err := s.Save(ctx, want); err != nil || len(ps) != 0 {
		t.Fatalf("save: %v %v", ps, err)
	}
	got, err := s.Page(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Brand.Subtitle != "Мой VPN" || got.CSS != "body{color:red}" {
		t.Fatalf("read back something else: %+v", got)
	}
	// The stored value is the document itself, not its text: anyone reading the setting
	// sees the same fields the API gives.
	raw, err := st.Q.GetSetting(ctx, settings.KeySubPage)
	if err != nil {
		t.Fatal(err)
	}
	var probe Page
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		t.Fatalf("stored value is not a document: %v (%s)", err, raw[:min(60, len(raw))])
	}
}

// A value written the old way — the document as a JSON string — is still read: the settings
// page opens with what the admin saved instead of answering 500.
func TestPageReadsTheOldWrappedString(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	set := settings.New(st.Q)

	want := Default()
	want.Brand.Subtitle = "старый формат"
	raw, _ := json.Marshal(want)
	if err := settings.Set(ctx, set, settings.KeySubPage, string(raw)); err != nil {
		t.Fatal(err)
	}
	got, err := NewService(st.Q).Page(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Brand.Subtitle != "старый формат" {
		t.Fatalf("the wrapped document was not read: %+v", got.Brand)
	}
}

// A value that is neither a document nor one wrapped: the page as it always was, and no
// error — a broken setting must not take the settings page down with it.
func TestPageSurvivesRubbish(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	set := settings.New(st.Q)

	if err := settings.Set(ctx, set, settings.KeySubPage, "не json вовсе"); err != nil {
		t.Fatal(err)
	}
	got, err := NewService(st.Q).Page(ctx)
	if err != nil {
		t.Fatalf("rubbish must not be an error: %v", err)
	}
	if len(got.Blocks) != len(Default().Blocks) {
		t.Fatalf("not the default page: %d blocks", len(got.Blocks))
	}
}

// A page with nothing stored yet is the default one.
func TestPageWithoutValue(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := NewService(st.Q).Page(ctx)
	if err != nil || len(got.Blocks) == 0 {
		t.Fatalf("default page: %v %d blocks", err, len(got.Blocks))
	}
}

// The page a subscriber opens carries no draft: a block turned off is gone, and the admin's
// CSS goes into a style of its own rather than into the data.
func TestPublicDropsOffBlocks(t *testing.T) {
	c := Default()
	for i := range c.Blocks {
		c.Blocks[i].On = true
	}
	c.Blocks[0].On = false
	c.CSS = "body{color:red}"
	pub := Public{Config: c, Brand: "Cozy", Accent: "#ff0000", css: c.CSS}
	pub.Config.CSS = ""
	pub.Config.Blocks = dropOff(c.Blocks)
	for _, b := range pub.Config.Blocks {
		if !b.On {
			t.Fatalf("an off block reached the page: %+v", b)
		}
	}
	if len(pub.Config.Blocks) != len(c.Blocks)-1 {
		t.Fatalf("got %d of %d blocks", len(pub.Config.Blocks), len(c.Blocks))
	}
	head := string(pub.Head())
	if !contains(head, "body{color:red}") {
		t.Fatal("the admin's CSS is not in the head")
	}
	if contains(head, "body{color:red}\",\"") {
		t.Fatal("the CSS went into the data as well")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
