package subpage

import (
	"bytes"
	"context"
	"encoding/json"
	"html"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store/db"
)

// Service reads the page's settings.
type Service struct {
	q   *db.Queries
	set *settings.Settings
}

func NewService(q *db.Queries) *Service { return &Service{q: q, set: settings.New(q)} }

// Page is the stored document over the defaults, cleaned: what does not fit is shown as
// the default (an older or a hand-edited document never breaks the page).
func (s *Service) Page(ctx context.Context) (Page, error) {
	c, _, err := settings.GetOver(ctx, s.set, settings.KeySubPage, Default())
	if err != nil {
		return Default(), err
	}
	_ = Normalize(&c)
	return c, nil
}

// Save stores a cleaned page and says what it had to refuse.
func (s *Service) Save(ctx context.Context, c Page) ([]Problem, error) {
	problems := Normalize(&c)
	if len(problems) > 0 {
		return problems, nil
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if err := settings.Set(ctx, s.set, settings.KeySubPage, string(raw)); err != nil {
		return nil, err
	}
	return nil, nil
}

// Public is what the page is told about itself: the document (its CSS goes into a style
// of its own), the brand and the colour it may take from the brand.
type Public struct {
	Config Page   `json:"config"`
	Brand  string `json:"brand"`
	// Accent is the brand's colour when the page takes it; "" keeps the palette's.
	Accent string `json:"accent,omitempty"`
	css    string
}

// Public gathers the page's data. brand and accent are the panel's.
func (s *Service) Public(ctx context.Context, brand, accent string) (Public, error) {
	c, err := s.Page(ctx)
	if err != nil {
		return Public{}, err
	}
	p := Public{Config: c, Brand: brand, css: c.CSS}
	p.Config.CSS = ""
	// A block turned off is the admin's draft: it does not go into the page's source.
	p.Config.Blocks = dropOff(c.Blocks)
	if c.Look.Accent == "brand" && ValidColor(accent) {
		p.Accent = accent
	}
	return p, nil
}

// dropOff is the blocks that show: the admin's own order, the built-in ones left out.
func dropOff(in []PageBlock) []PageBlock {
	out := make([]PageBlock, 0, len(in))
	for _, b := range in {
		if b.On {
			out = append(out, b)
		}
	}
	return out
}

// Head is what goes into the page's <head>: the page's data for its script (a JSON block,
// which the browser does not run) and the admin's own CSS.
func (p Public) Head() []byte {
	var b bytes.Buffer
	data, _ := json.Marshal(p)
	b.WriteString(`<script type="application/json" id="cozy-page">`)
	b.Write(data)
	b.WriteString(`</script>`)
	if css := CleanCSS(p.css); css != "" {
		b.WriteString(`<style id="cozy-css">`)
		b.WriteString(css)
		b.WriteString(`</style>`)
	}
	return b.Bytes()
}

// DocHTML escapes a string for HTML, for the bits of the page built here.
func DocHTML(s string) string { return html.EscapeString(s) }
