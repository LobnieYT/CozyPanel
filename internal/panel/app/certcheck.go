package app

import (
	"context"
	"net/netip"

	"cozy/internal/panel/acme"
	"cozy/internal/panel/certcheck"
	"cozy/internal/panel/domain"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
)

// certCheck builds the outside look at the panel's certificate: what its ports serve,
// whether the domain leads here, whether the CA's port 80 is free, and what the nodes
// present. certs may be nil in development: then only the address is checked.
func certCheck(st *store.Store, certs *acme.Manager, resolve func(context.Context, string) ([]netip.Addr, error)) func(context.Context) (*certcheck.CertReport, error) {
	return func(ctx context.Context) (*certcheck.CertReport, error) {
		set := settings.New(st.Q)
		domain_, _ := set.String(ctx, settings.KeyDomain)
		host, _ := set.String(ctx, settings.KeyPublicHost)
		in := certcheck.Input{Domain: domain_, Host: host}
		if port, _, err := settings.Get[int](ctx, set, settings.KeyPanelPort); err == nil && port > 0 {
			in.Ports = append(in.Ports, port)
		}
		if port, _, err := settings.Get[int](ctx, set, settings.KeySubPort); err == nil && port > 0 {
			in.Ports = append(in.Ports, port)
		}
		if certs != nil {
			in.Status = certs.Status()
			if leaf := certs.Leaf(); leaf != nil && leaf.Leaf != nil {
				in.Expected = leaf.Leaf
			}
		}
		rows, err := st.Q.ListNodes(ctx)
		if err != nil {
			return nil, err
		}
		for _, n := range rows {
			node := certcheck.Node{ID: n.ID, Name: n.Name, Local: n.Address == "", SNI: n.Domain}
			if node.Local {
				node.Host, node.SNI = host, domain_
			} else {
				node.Host = domain.NodeHost(n)
			}
			in.Nodes = append(in.Nodes, node)
		}
		report := certcheck.New(resolverAdapter{resolve}).Run(ctx, in)
		return &report, nil
	}
}

// resolverAdapter hands the checker the panel's resolver.
type resolverAdapter struct {
	resolve func(context.Context, string) ([]netip.Addr, error)
}

func (r resolverAdapter) Lookup(ctx context.Context, name string) ([]netip.Addr, error) {
	if r.resolve == nil {
		return domain.SystemResolve(ctx, name)
	}
	return r.resolve(ctx, name)
}
