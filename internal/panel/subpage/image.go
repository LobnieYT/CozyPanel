package subpage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
)

// The page's logo: one picture, uploaded by the admin, shown on the subscription page.
const AssetLogo = "logo"

// AssetLimit is how big the logo may be, in bytes; 0: no such image.
func AssetLimit(name string) int {
	if name == AssetLogo {
		return 512 << 10
	}
	return 0
}

var (
	// ErrImageType: the bytes are not PNG, JPEG or WebP (an SVG, which may carry script,
	// or an HTML page named .png among them).
	ErrImageType = errors.New("image_type")
	// ErrImageTooBig: over AssetLimit.
	ErrImageTooBig = errors.New("image_too_big")
)

// CheckImage says what kind of image data is, the way a browser would sniff it, and takes
// only PNG, JPEG and WebP within the image's limit: the type is fixed by what the bytes
// are, not by a name or a header someone sent.
func CheckImage(name string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", ErrImageType
	}
	if len(data) > AssetLimit(name) {
		return "", ErrImageTooBig
	}
	switch ct := http.DetectContentType(data); ct {
	case "image/png", "image/jpeg", "image/webp":
		return ct, nil
	}
	return "", ErrImageType
}

// Hash names a version of the image in its links.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// AssetPath is where the page finds the logo, relative to the subscription path: the
// hash makes a new upload a new address, so browsers do not show the old one.
func AssetPath(name, hash string) string { return "brand/" + name + "?v=" + hash }

// Ext is the file name's ending for the image's type, "" for one it does not serve.
func Ext(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ""
}

// PageAsset is the logo without its bytes: what the settings page needs to show it and what
// the page needs to link it.
type PageAsset struct {
	Name      string `json:"name"`
	Type      string `json:"content_type"`
	Hash      string `json:"hash"`
	Size      int64  `json:"size"`
	UpdatedAt int64  `json:"updated_at"`
}
