package node

import (
	"context"
	"os"
	"sync"

	"github.com/metacubex/mihomo/component/geodata"
	C "github.com/metacubex/mihomo/constant"

	"cozy/internal/geox"
	"cozy/internal/nodeapi"
)

// geoFiles are the geodata files the node's GEO rules and policies read,
// with the URLs they come from.
var geoFiles = []struct {
	name string
	path func() string
	url  string
}{
	{"geosite", C.Path.GeoSite, geox.URL["geosite"]},
	{"geoip", C.Path.GeoIP, geox.URL["geoip"]},
	{"asn", C.Path.ASN, geox.URL["asn"]},
}

// GeoStatus lists the geodata files: present, size and age.
func (e *Engine) GeoStatus() nodeapi.GeoStatus {
	st := nodeapi.GeoStatus{Files: []nodeapi.GeoFile{}}
	for _, f := range geoFiles {
		g := nodeapi.GeoFile{Name: f.name, URL: f.url}
		if fi, err := os.Stat(f.path()); err == nil && !fi.IsDir() {
			g.Present = true
			g.Size = fi.Size()
			g.UpdatedAt = fi.ModTime().UTC()
		}
		st.Files = append(st.Files, g)
	}
	return st
}

var geoUpdateMu sync.Mutex

// UpdateGeoData re-downloads every geodata file, even a present one: the files
// go away, the memory caches drop, and mihomo fetches them again. In-flight
// matches keep working on what they hold; new ones read the fresh files.
func (e *Engine) UpdateGeoData() error {
	geoUpdateMu.Lock()
	defer geoUpdateMu.Unlock()
	for _, f := range geoFiles {
		if err := os.Remove(f.path()); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	geodata.ClearGeoIPCache()
	geodata.ClearGeoSiteCache()
	if err := e.ensureGeoData(context.Background()); err != nil {
		return err
	}
	return nil
}

// ensureGeoData points mihomo at the geodata URLs and fetches what is missing.
func (e *Engine) ensureGeoData(ctx context.Context) error {
	_ = ctx
	geodata.SetGeodataMode(true)
	geodata.SetGeoIpUrl(geox.URL["mmdb"])
	geodata.SetGeoSiteUrl(geox.URL["geosite"])
	geodata.SetASNUrl(geox.URL["asn"])
	if err := geodata.InitGeoIP(); err != nil {
		return err
	}
	if err := geodata.InitGeoSite(); err != nil {
		return err
	}
	return geodata.InitASN()
}
