package subpage

import "testing"

// The logo is taken by what the bytes are, not by what the browser says they are: an SVG
// (which may carry script) and an HTML page named .png are both refused.
func TestCheckImage(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	jpeg := append([]byte("\xff\xd8\xff\xe0"), make([]byte, 64)...)
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 64)...)
	for name, c := range map[string]struct {
		asset string
		data  []byte
		want  string
		err   error
	}{
		"png":   {AssetLogo, png, "image/png", nil},
		"jpeg":  {AssetLogo, jpeg, "image/jpeg", nil},
		"webp":  {AssetLogo, webp, "image/webp", nil},
		"svg":   {AssetLogo, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), "", ErrImageType},
		"html":  {AssetLogo, []byte("<!doctype html><script>alert(1)</script>"), "", ErrImageType},
		"empty": {AssetLogo, nil, "", ErrImageType},
		"over":  {AssetLogo, make([]byte, AssetLimit(AssetLogo)+1), "", ErrImageTooBig},
		// A name the panel has no image for has no room either: nothing is served.
		"other": {"background", png, "", ErrImageTooBig},
	} {
		got, err := CheckImage(c.asset, c.data)
		if got != c.want || err != c.err {
			t.Errorf("CheckImage(%s) = %q, %v; want %q, %v", name, got, err, c.want, c.err)
		}
	}
}

// The picture's address carries its hash: a new upload is a new address, so a browser does
// not keep showing the old one.
func TestHashAndPath(t *testing.T) {
	if AssetLimit(AssetLogo) != 512<<10 {
		t.Fatalf("logo limit: %d", AssetLimit(AssetLogo))
	}
	h := Hash([]byte("one"))
	if h == "" || h == Hash([]byte("two")) || h != Hash([]byte("one")) {
		t.Fatalf("hash: %q", h)
	}
	if got := AssetPath(AssetLogo, h); got != "brand/logo?v="+h {
		t.Fatalf("path: %q", got)
	}
	for ct, want := range map[string]string{
		"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": "",
	} {
		if got := Ext(ct); got != want {
			t.Errorf("Ext(%q) = %q, want %q", ct, got, want)
		}
	}
}
