package settings

import "strings"

// CanonicalDomain is a domain name as the world knows it: lower case and without a
// trailing dot. A DNS name does not care about the case, and the ACME server brings the
// name of an order to the lower case (RFC 8555 §7.4) — a domain written with capitals
// ("Alpha-Sun-X2uTG1QW.example.com") is then "changed" by the server, and the client
// library, which compares the identifiers letter by letter, refuses the order.
//
// One spelling, everywhere: the settings, the certificate, the nodes' SNI and the links
// of the subscriptions all name the same host.
func CanonicalDomain(d string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
}
