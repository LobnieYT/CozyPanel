package nodeapi

import "strings"

// AdBlockRules renders the block: exceptions leave directly, then the custom
// domains and the category list are rejected. target is REJECT or REJECT-DROP.
func AdBlockRules(d *AdBlock) []string {
	if d == nil || !d.Enabled {
		return nil
	}
	target := "REJECT"
	if d.Drop {
		target = "REJECT-DROP"
	}
	out := []string{}
	for _, s := range d.Exceptions {
		out = append(out, "DOMAIN-SUFFIX,"+strings.TrimPrefix(strings.TrimSpace(s), ".")+",DIRECT")
	}
	for _, s := range d.Extra {
		out = append(out, "DOMAIN-SUFFIX,"+strings.TrimPrefix(strings.TrimSpace(s), ".")+","+target)
	}
	return append(out, "GEOSITE,category-ads-all,"+target)
}
