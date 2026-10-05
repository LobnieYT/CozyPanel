package node

import (
	"github.com/metacubex/mihomo/adapter/inbound"
	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/component/sniffer"
	"github.com/metacubex/mihomo/component/trie"
	mihomocfg "github.com/metacubex/mihomo/config"
	"github.com/metacubex/mihomo/dns"
	"github.com/metacubex/mihomo/tunnel"
)

// applyNetGlobals applies what executor.ApplyConfig applies besides listeners,
// proxies and rules, which Apply manages incrementally so connections survive:
// the DNS resolvers, the hosts table and the sniffer.
//
// Without this the globals stay as Start left them (DNS off, resolvers nil):
// every dial falls back to the system resolver, the DNS policy never touches
// real traffic, hosts never apply and sniffing stays dormant — while the
// testers, which match directly, keep reporting green.
// This mirrors hub/executor.updateDNS, updateHosts and the sniffer part of
// ApplyConfig line for line; when mihomo changes them, this follows.
//
// egressIPv6 comes from the panel's egress_ipv6 switch (off by default):
// strictly IPv4 egress, one identity for anti-abuse systems.
func applyNetGlobals(cfg *mihomocfg.Config, egressIPv6 bool) {
	applyHosts(cfg.Hosts)
	applySniffer(cfg.Sniffer)
	resolver.DisableIPv6 = !egressIPv6
	applyDNS(cfg.DNS, cfg.General.IPv6)
}

func applyHosts(tree *trie.DomainTrie[resolver.HostValue]) {
	resolver.DefaultHosts = resolver.NewHosts(tree)
}

func applySniffer(snifferConfig *sniffer.Config) {
	dispatcher, _ := sniffer.NewDispatcher(snifferConfig)
	if dispatcher == nil {
		dispatcher = &sniffer.Dispatcher{}
	}
	tunnel.UpdateSniffer(dispatcher)
}

func applyDNS(c *mihomocfg.DNS, generalIPv6 bool) {
	if !c.Enable {
		resolver.DefaultResolver = nil
		resolver.DefaultHostMapper = nil
		resolver.DefaultService = nil
		resolver.ProxyServerHostResolver = nil
		resolver.DirectHostResolver = nil
		dns.ReCreateServer("", nil, nil)
		return
	}

	ipv6 := c.IPv6 && generalIPv6
	r := dns.NewResolver(dns.Config{
		Main:                 c.NameServer,
		Fallback:             c.Fallback,
		IPv6:                 ipv6,
		IPv6Timeout:          c.IPv6Timeout,
		FallbackIPFilter:     c.FallbackIPFilter,
		FallbackDomainFilter: c.FallbackDomainFilter,
		FallbackLazyQuery:    c.FallbackLazyQuery,
		Default:              c.DefaultNameserver,
		Policy:               c.NameServerPolicy,
		ProxyServer:          c.ProxyServerNameserver,
		ProxyServerPolicy:    c.ProxyServerPolicy,
		DirectServer:         c.DirectNameServer,
		DirectFollowPolicy:   c.DirectFollowPolicy,
		CacheAlgorithm:       c.CacheAlgorithm,
		CacheMaxSize:         c.CacheMaxSize,
	})
	m := dns.NewEnhancer(dns.EnhancerConfig{
		IPv6:          ipv6,
		EnhancedMode:  c.EnhancedMode,
		FakeIPPool:    c.FakeIPPool,
		FakeIPPool6:   c.FakeIPPool6,
		FakeIPSkipper: c.FakeIPSkipper,
		FakeIPTTL:     c.FakeIPTTL,
		UseHosts:      c.UseHosts,
	})

	// reuse cache of old host mapper
	if old := resolver.DefaultHostMapper; old != nil {
		m.PatchFrom(old.(*dns.ResolverEnhancer))
	}

	s := dns.NewService(r, m)

	resolver.DefaultResolver = r
	resolver.DefaultHostMapper = m
	resolver.DefaultService = s
	resolver.UseSystemHosts = c.UseSystemHosts

	if r.ProxyResolver.Invalid() {
		resolver.ProxyServerHostResolver = r.ProxyResolver
	} else {
		resolver.ProxyServerHostResolver = r.Resolver
	}

	if r.DirectResolver.Invalid() {
		resolver.DirectHostResolver = r.DirectResolver
	} else {
		resolver.DirectHostResolver = r.Resolver
	}

	lc := inbound.NewListenConfig()
	lc.SetRouteMark(c.ListenRoutingMark)
	dns.ReCreateServer(c.Listen, lc, s)
}
