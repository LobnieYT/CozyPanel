// Package geox holds the geodata Koala Clash ships (MetaCubeX meta-rules-dat,
// mihomo's own): client profiles point at it, and nodes download from it when
// their rules or DNS policies reference GEO data.
package geox

// URL is the download map mihomo's geox-url takes. GeoIP.dat is the full
// v2ray-format base, exactly mihomo's own default: the lite and MaxMind builds
// parse as "invalid wire-format" and poison every GEOIP rule until replaced.
var URL = map[string]string{
	"geoip":   "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat",
	"geosite": "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat",
	"mmdb":    "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.metadb",
	"asn":     "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/GeoLite2-ASN.mmdb",
}
