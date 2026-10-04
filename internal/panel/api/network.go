package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"cozy/internal/nodeapi"
	"cozy/internal/panel/netcfg"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/store/db"
)

// Network documents: the global DNS, routes and outbounds nodes inherit, and the
// subscription DNS client profiles carry. Raw JSON/text as stored; "" is unset.
type NetworkView struct {
	NodeDNS       string `json:"node_dns" doc:"DNS нод: JSON NodeDNS; пусто — резолвер mihomo выключен"`
	NodeRoutes    string `json:"node_routes" doc:"Маршруты нод: по правилу mihomo на строку"`
	NodeOutbounds string `json:"node_outbounds" doc:"Исходящие нод: JSON-массив прокси mihomo"`
	SubDNS        string `json:"sub_dns" doc:"DNS подписок: JSON SubDNS; пусто — встроенный DNS профилей"`
	NodeAdBlock   string `json:"node_adblock" doc:"Антиреклама нод: JSON AdBlock; пусто — выключена"`
	SubAdBlock    string `json:"sub_adblock" doc:"Антиреклама подписок: JSON AdBlock; пусто — выключена"`
}

type networkOutput struct{ Body NetworkView }

type patchNetworkInput struct {
	Body struct {
		NodeDNS       *string `json:"node_dns,omitempty"`
		NodeRoutes    *string `json:"node_routes,omitempty"`
		NodeOutbounds *string `json:"node_outbounds,omitempty"`
		SubDNS        *string `json:"sub_dns,omitempty"`
		NodeAdBlock   *string `json:"node_adblock,omitempty"`
		SubAdBlock    *string `json:"sub_adblock,omitempty"`
	}
}

// NodeNetworkView is one node's overrides and what they resolve to with globals.
type NodeNetworkView struct {
	DNSOverride        string                 `json:"dns_override" doc:"Пусто — наследовать глобальный node_dns"`
	RoutesOverride     string                 `json:"routes_override" doc:"Пусто — наследовать глобальный node_routes"`
	OutboundsOverride  string                 `json:"outbounds_override" doc:"Пусто — наследовать глобальный node_outbounds"`
	EffectiveDNS       *nodeapi.NodeDNS       `json:"effective_dns"`
	EffectiveDefault   string                 `json:"effective_default" doc:"MATCH для несработавшего: DIRECT по умолчанию"`
	EffectiveRoutes    []nodeapi.NodeRoute    `json:"effective_routes"`
	EffectiveOutbounds []nodeapi.NodeOutbound `json:"effective_outbounds"`
}

type nodeNetworkOutput struct{ Body NodeNetworkView }

type patchNodeNetworkInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		DNSOverride       *string `json:"dns_override,omitempty"`
		RoutesOverride    *string `json:"routes_override,omitempty"`
		OutboundsOverride *string `json:"outbounds_override,omitempty"`
	}
}

type routeTestInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Domain  string `json:"domain,omitempty" maxLength:"253"`
		IP      string `json:"ip,omitempty" maxLength:"45"`
		Port    int    `json:"port,omitempty" minimum:"0" maximum:"65535"`
		Network string `json:"network,omitempty" enum:"tcp,udp"`
		Inbound string `json:"inbound,omitempty" maxLength:"64"`
	}
}

type routeTestMatch struct {
	Matched bool   `json:"matched"`
	Rule    string `json:"rule,omitempty"`
	Target  string `json:"target,omitempty"`
	// GeoSkipped: a GEO rule stands before the match (dry only), so a live test
	// on the node may still judge otherwise.
	GeoSkipped bool `json:"geo_skipped,omitempty"`
}

type routeTestOutput struct {
	Body struct {
		Dry  routeTestMatch  `json:"dry" doc:"Сухая проверка в панели, без ноды"`
		Live *routeTestMatch `json:"live,omitempty" doc:"Прогон через mihomo ноды; null — нода недоступна"`
	}
}

type probeInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Proxy string `json:"proxy" minLength:"1" maxLength:"64" doc:"Имя исходящего: DIRECT, WARP, NODE-<id> или свой"`
	}
}

type probeOutput struct {
	Body ProbeView
}

type importOutboundInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"64" doc:"Имя нового исходящего"`
		Conf string `json:"conf" minLength:"1" maxLength:"8192" doc:"WireGuard .conf: секции [Interface] и [Peer]"`
	}
}

func (h *handlers) registerNetwork() {
	huma.Register(h.api, huma.Operation{OperationID: "get-network", Method: http.MethodGet, Path: "/api/v1/network", Summary: "Сетевые настройки (DNS, маршруты, исходящие)", Tags: []string{"network"}}, h.getNetwork)
	huma.Register(h.api, huma.Operation{OperationID: "update-network", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPatch, Path: "/api/v1/network", Summary: "Изменить сетевые настройки", Tags: []string{"network"}}, h.updateNetwork)
	huma.Register(h.api, huma.Operation{OperationID: "get-node-network", Method: http.MethodGet, Path: "/api/v1/nodes/{id}/network", Summary: "Сеть ноды: оверрайды и итог", Tags: []string{"node"}}, h.getNodeNetwork)
	huma.Register(h.api, huma.Operation{OperationID: "update-node-network", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPatch, Path: "/api/v1/nodes/{id}/network", Summary: "Изменить сеть ноды", Tags: []string{"node"}}, h.updateNodeNetwork)
	huma.Register(h.api, huma.Operation{OperationID: "test-node-route", Method: http.MethodPost, Path: "/api/v1/nodes/{id}/route-test", Summary: "Проверить маршрут: сухое и живое", Tags: []string{"node"}}, h.testNodeRoute)
	huma.Register(h.api, huma.Operation{OperationID: "probe-node-outbound", Method: http.MethodPost, Path: "/api/v1/nodes/{id}/probe", Summary: "Проверить исходящее ноды", Tags: []string{"node"}}, h.probeNodeOutbound)
	huma.Register(h.api, huma.Operation{OperationID: "import-node-outbound", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPost, Path: "/api/v1/nodes/{id}/outbounds/import", Summary: "Импорт исходящего из WireGuard .conf", Tags: []string{"node"}}, h.importNodeOutbound)
	huma.Register(h.api, huma.Operation{OperationID: "node-geo", Method: http.MethodGet, Path: "/api/v1/nodes/{id}/geo", Summary: "Геоданные ноды", Tags: []string{"node"}}, h.nodeGeo)
	huma.Register(h.api, huma.Operation{OperationID: "update-node-geo", Metadata: sessionOnly, Extensions: sessionOnlyExt, Method: http.MethodPost, Path: "/api/v1/nodes/{id}/geo/update", Summary: "Обновить геоданные ноды", Tags: []string{"node"}}, h.updateNodeGeo)
}

func (h *handlers) getNetwork(ctx context.Context, _ *struct{}) (*networkOutput, error) {
	v, err := h.readNetwork(ctx)
	if err != nil {
		return nil, err
	}
	return &networkOutput{Body: v}, nil
}

func (h *handlers) readNetwork(ctx context.Context) (NetworkView, error) {
	var v NetworkView
	var err error
	get := func(key string, dst *string) {
		if err == nil {
			*dst, _, err = settings.Get[string](ctx, h.d.Settings, key)
		}
	}
	get(settings.KeyNodeDNS, &v.NodeDNS)
	get(settings.KeyNodeRoutes, &v.NodeRoutes)
	get(settings.KeyNodeOutbounds, &v.NodeOutbounds)
	get(settings.KeySubDNS, &v.SubDNS)
	get(settings.KeyNodeAdBlock, &v.NodeAdBlock)
	get(settings.KeySubAdBlock, &v.SubAdBlock)
	if err != nil {
		return v, err
	}
	return v, nil
}

func (h *handlers) updateNetwork(ctx context.Context, in *patchNetworkInput) (*networkOutput, error) {
	b := in.Body
	for field, touched := range map[string]bool{"node_dns": b.NodeDNS != nil, "node_routes": b.NodeRoutes != nil, "node_outbounds": b.NodeOutbounds != nil, "sub_dns": b.SubDNS != nil, "node_adblock": b.NodeAdBlock != nil, "sub_adblock": b.SubAdBlock != nil} {
		if touched {
			if err := requireSession(ctx, field); err != nil {
				return nil, err
			}
		}
	}
	if details := checkNetworkDocs(b.NodeDNS, b.NodeRoutes, b.NodeOutbounds, b.SubDNS, b.NodeAdBlock, b.SubAdBlock); len(details) > 0 {
		return nil, huma.Error422UnprocessableEntity("validation", details...)
	}
	// Cross-check routes against the effective outbounds: mihomo resolves rule
	// targets lazily, so a typo would pass its parser and kill the matching
	// traffic at runtime instead of failing the save.
	if b.NodeRoutes != nil || b.NodeOutbounds != nil {
		glob, err := h.readNetwork(ctx)
		if err != nil {
			return nil, err
		}
		rawOut := glob.NodeOutbounds
		if b.NodeOutbounds != nil {
			rawOut = *b.NodeOutbounds
		}
		rawRoutes := glob.NodeRoutes
		if b.NodeRoutes != nil {
			rawRoutes = *b.NodeRoutes
		}
		outDoc, err := netcfg.ParseNodeOutbounds(rawOut)
		if err == nil {
			if _, err := netcfg.ParseNodeRoutesChecked(rawRoutes, netcfg.RouteTargetKnown(outDoc)); err != nil {
				return nil, huma.Error422UnprocessableEntity("validation", netDetail("body.node_routes", err))
			}
		}
	}
	err := h.d.Store.Tx(ctx, func(q *db.Queries) error {
		set := settings.New(q)
		for key, v := range map[string]*string{settings.KeyNodeDNS: b.NodeDNS, settings.KeyNodeRoutes: b.NodeRoutes, settings.KeyNodeOutbounds: b.NodeOutbounds, settings.KeySubDNS: b.SubDNS, settings.KeyNodeAdBlock: b.NodeAdBlock, settings.KeySubAdBlock: b.SubAdBlock} {
			if v == nil {
				continue
			}
			if err := settings.Set(ctx, set, key, strings.TrimSpace(*v)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	h.audit(ctx, sessionOf(ctx).AdminID, "network.update", "", "", nil)
	h.d.Changes.SlotsChanged()
	v, err := h.readNetwork(ctx)
	if err != nil {
		return nil, err
	}
	return &networkOutput{Body: v}, nil
}

// checkNetworkDocs parses the provided documents the way nodesync will.
func checkNetworkDocs(dns, routes, outbounds, sub, nodeAb, subAb *string) []error {
	var details []error
	if dns != nil {
		if _, err := netcfg.ParseNodeDNS(*dns); err != nil {
			details = append(details, netDetail("body.node_dns", err))
		}
	}
	if routes != nil {
		if _, err := netcfg.ParseNodeRoutes(*routes); err != nil {
			details = append(details, netDetail("body.node_routes", err))
		}
	}
	if outbounds != nil {
		if _, err := netcfg.ParseNodeOutbounds(*outbounds); err != nil {
			details = append(details, netDetail("body.node_outbounds", err))
		}
	}
	if sub != nil {
		if _, err := netcfg.ParseSubDNS(*sub); err != nil {
			details = append(details, netDetail("body.sub_dns", err))
		}
	}
	if nodeAb != nil {
		if _, err := netcfg.ParseAdBlock(*nodeAb); err != nil {
			details = append(details, netDetail("body.node_adblock", err))
		}
	}
	if subAb != nil {
		if _, err := netcfg.ParseAdBlock(*subAb); err != nil {
			details = append(details, netDetail("body.sub_adblock", err))
		}
	}
	return details
}

func netDetail(loc string, err error) error {
	field, code, line := netcfg.Detail(err)
	if code == "invalid" {
		return &huma.ErrorDetail{Location: loc, Message: err.Error()}
	}
	d := &huma.ErrorDetail{Location: loc + "." + field, Message: code}
	if line > 0 {
		d.Value = line
	}
	return d
}

func (h *handlers) nodeOr404(ctx context.Context, id int64) (db.Node, error) {
	n, err := h.d.Store.Q.GetNode(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return db.Node{}, huma.Error404NotFound("not_found")
	}
	return n, err
}

func (h *handlers) getNodeNetwork(ctx context.Context, in *userIDInput) (*nodeNetworkOutput, error) {
	n, err := h.nodeOr404(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	v, err := h.nodeNetworkView(ctx, n)
	if err != nil {
		return nil, err
	}
	return &nodeNetworkOutput{Body: v}, nil
}

func (h *handlers) nodeNetworkView(ctx context.Context, n db.Node) (NodeNetworkView, error) {
	v := NodeNetworkView{
		DNSOverride:        n.DnsOverride.String,
		RoutesOverride:     n.RoutesOverride.String,
		OutboundsOverride:  n.OutboundsOverride.String,
		EffectiveRoutes:    []nodeapi.NodeRoute{},
		EffectiveOutbounds: []nodeapi.NodeOutbound{},
	}
	res, err := netcfg.Resolve(ctx, h.d.Store.Q, h.d.Settings, n)
	if err != nil {
		return v, err
	}
	v.EffectiveDNS = res.DNS
	v.EffectiveDefault = res.Routes.Default
	v.EffectiveRoutes = res.Routes.Rules
	if v.EffectiveRoutes == nil {
		v.EffectiveRoutes = []nodeapi.NodeRoute{}
	}
	v.EffectiveOutbounds = res.Outbounds
	if v.EffectiveOutbounds == nil {
		v.EffectiveOutbounds = []nodeapi.NodeOutbound{}
	}
	return v, nil
}

func (h *handlers) updateNodeNetwork(ctx context.Context, in *patchNodeNetworkInput) (*nodeNetworkOutput, error) {
	n, err := h.nodeOr404(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	b := in.Body
	for field, touched := range map[string]bool{"dns_override": b.DNSOverride != nil, "routes_override": b.RoutesOverride != nil, "outbounds_override": b.OutboundsOverride != nil} {
		if touched {
			if err := requireSession(ctx, field); err != nil {
				return nil, err
			}
		}
	}
	dns, routes, outbounds := n.DnsOverride.String, n.RoutesOverride.String, n.OutboundsOverride.String
	if b.DNSOverride != nil {
		dns = strings.TrimSpace(*b.DNSOverride)
	}
	if b.RoutesOverride != nil {
		routes = strings.TrimSpace(*b.RoutesOverride)
	}
	if b.OutboundsOverride != nil {
		outbounds = strings.TrimSpace(*b.OutboundsOverride)
	}
	// Structural check first; then mihomo itself on the node when it is online.
	glob, err := h.readNetwork(ctx)
	if err != nil {
		return nil, err
	}
	eff := func(ov, g string) string {
		if strings.TrimSpace(ov) != "" {
			return ov
		}
		return g
	}
	dnsDoc, err := netcfg.ParseNodeDNS(eff(dns, glob.NodeDNS))
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("validation", netDetail("body.dns_override", err))
	}
	outDoc, err := netcfg.ParseNodeOutbounds(eff(outbounds, glob.NodeOutbounds))
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("validation", netDetail("body.outbounds_override", err))
	}
	// Targets naming no proxy would pass mihomo's parser and kill the matching
	// traffic at runtime: refuse them before the node roundtrip (which an offline
	// node skips), with the offending line.
	routesDoc, err := netcfg.ParseNodeRoutesChecked(eff(routes, glob.NodeRoutes), netcfg.RouteTargetKnown(outDoc))
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("validation", netDetail("body.routes_override", err))
	}
	if h.d.Nodes != nil {
		req := nodeapi.ValidateNetRequest{DNS: dnsDoc, Routes: routesDoc, Outbounds: outDoc}
		if verr := h.d.Nodes.ValidateNet(ctx, n.ID, req); verr != nil && !errors.Is(verr, nodeapi.ErrUnavailable) {
			return nil, huma.Error422UnprocessableEntity("validation", &huma.ErrorDetail{Location: "body", Message: verr.Error()})
		}
	}
	if err := h.d.Store.Tx(ctx, func(q *db.Queries) error {
		return q.SetNodeNet(ctx, db.SetNodeNetParams{DnsOverride: nullStr(dns), RoutesOverride: nullStr(routes), OutboundsOverride: nullStr(outbounds), UpdatedAt: h.d.Now().Unix(), ID: n.ID})
	}); err != nil {
		return nil, err
	}
	h.audit(ctx, sessionOf(ctx).AdminID, "node.network.update", "node", strconv.FormatInt(n.ID, 10), nil)
	h.d.Changes.SlotsChanged()
	// Re-read: the response must carry what was saved, not the pre-update node.
	n, err = h.nodeOr404(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	v, err := h.nodeNetworkView(ctx, n)
	if err != nil {
		return nil, err
	}
	return &nodeNetworkOutput{Body: v}, nil
}

// nullStr stores "": NULL reads back as "" through sql.NullString either way.
func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func (h *handlers) testNodeRoute(ctx context.Context, in *routeTestInput) (*routeTestOutput, error) {
	n, err := h.nodeOr404(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	b := in.Body
	if b.Domain == "" && b.IP == "" {
		return nil, huma.Error422UnprocessableEntity("validation", &huma.ErrorDetail{Location: "body", Message: "domain_or_ip"})
	}
	out := &routeTestOutput{}
	res, err := netcfg.Resolve(ctx, h.d.Store.Q, h.d.Settings, n)
	if err != nil {
		return nil, err
	}
	abRaw, _, err := settings.Get[string](ctx, h.d.Settings, settings.KeyNodeAdBlock)
	if err != nil {
		return nil, err
	}
	ab, err := netcfg.ParseAdBlock(abRaw)
	if err != nil {
		return nil, err
	}
	rules := append(append([]nodeapi.NodeRoute{}, res.Routes.Rules...), nodeRouteOf(nodeapi.AdBlockRules(ab))...)
	rule, target, matched, geo := netcfg.MatchRoute(rules, netcfg.RouteInput{Domain: b.Domain, IP: b.IP, Port: b.Port, Network: b.Network, Inbound: b.Inbound})
	out.Body.Dry = routeTestMatch{Matched: matched, Rule: rule, Target: target, GeoSkipped: geo}
	if h.d.Nodes != nil {
		live, lerr := h.d.Nodes.RouteTest(ctx, n.ID, nodeapi.RouteTestRequest{Domain: b.Domain, IP: b.IP, Port: b.Port, Network: b.Network, Inbound: b.Inbound})
		if lerr == nil {
			out.Body.Live = &routeTestMatch{Matched: live.Matched, Rule: live.Rule, Target: live.Target}
		}
	}
	return out, nil
}

// nodeRouteOf lifts rendered rule strings into NodeRoute values.
func nodeRouteOf(rules []string) []nodeapi.NodeRoute {
	out := make([]nodeapi.NodeRoute, 0, len(rules))
	for _, r := range rules {
		out = append(out, nodeapi.NodeRoute{Rule: r})
	}
	return out
}

func (h *handlers) probeNodeOutbound(ctx context.Context, in *probeInput) (*probeOutput, error) {
	n, err := h.nodeOr404(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if h.d.Nodes == nil {
		return nil, huma.Error409Conflict("nodes_disabled")
	}
	p, err := h.d.Nodes.Probe(ctx, n.ID, in.Body.Proxy)
	if err != nil {
		if errors.Is(err, nodeapi.ErrUnavailable) {
			return nil, huma.Error409Conflict("nodes_disabled")
		}
		return nil, err
	}
	return &probeOutput{Body: ProbeView{OK: p.OK, IP: p.IP, Colo: p.Colo, Error: p.Error, CheckedAt: p.CheckedAt}}, nil
}

// importNodeOutbound parses a WireGuard .conf and appends it to the node's
// effective outbounds, saving the result as the node's override.
func (h *handlers) importNodeOutbound(ctx context.Context, in *importOutboundInput) (*nodeNetworkOutput, error) {
	n, err := h.nodeOr404(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := requireSession(ctx, "outbounds_override"); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Body.Name)
	if name == "" || strings.ContainsAny(name, " \t,") || len(name) > 64 {
		return nil, huma.Error422UnprocessableEntity("validation", &huma.ErrorDetail{Location: "body.name", Message: "bad_name"})
	}
	ob, err := netcfg.WireGuardConf(name, in.Body.Conf)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("validation", &huma.ErrorDetail{Location: "body.conf", Message: err.Error()})
	}
	res, err := netcfg.Resolve(ctx, h.d.Store.Q, h.d.Settings, n)
	if err != nil {
		return nil, err
	}
	for _, o := range res.Outbounds {
		if strings.EqualFold(o.Name, name) {
			return nil, huma.Error422UnprocessableEntity("validation", &huma.ErrorDetail{Location: "body.name", Message: "name_in_use"})
		}
	}
	// The stored doc is a flat array of proxy maps; the import appends to the
	// effective one and saves the result as the node's override.
	var docs []any
	rawDoc := n.OutboundsOverride.String
	if strings.TrimSpace(rawDoc) == "" {
		rawDoc, err = h.d.Settings.String(ctx, settings.KeyNodeOutbounds)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(rawDoc) != "" {
		if err := json.Unmarshal([]byte(rawDoc), &docs); err != nil {
			return nil, huma.Error422UnprocessableEntity("validation", netDetail("body.conf", err))
		}
	}
	var flat map[string]any
	if err := json.Unmarshal(ob.Config, &flat); err != nil {
		return nil, huma.Error422UnprocessableEntity("validation", &huma.ErrorDetail{Location: "body.conf", Message: err.Error()})
	}
	docs = append(docs, flat)
	raw, err := json.Marshal(docs)
	if err != nil {
		return nil, err
	}
	outbounds := string(raw)
	if _, err := netcfg.ParseNodeOutbounds(outbounds); err != nil {
		return nil, huma.Error422UnprocessableEntity("validation", netDetail("body.conf", err))
	}
	if h.d.Nodes != nil {
		req := nodeapi.ValidateNetRequest{DNS: res.DNS, Routes: res.Routes, Outbounds: append(append([]nodeapi.NodeOutbound{}, res.Outbounds...), ob)}
		if verr := h.d.Nodes.ValidateNet(ctx, n.ID, req); verr != nil && !errors.Is(verr, nodeapi.ErrUnavailable) {
			return nil, huma.Error422UnprocessableEntity("validation", &huma.ErrorDetail{Location: "body.conf", Message: verr.Error()})
		}
	}
	if err := h.d.Store.Tx(ctx, func(q *db.Queries) error {
		return q.SetNodeNet(ctx, db.SetNodeNetParams{DnsOverride: n.DnsOverride, RoutesOverride: n.RoutesOverride, OutboundsOverride: nullStr(outbounds), UpdatedAt: h.d.Now().Unix(), ID: n.ID})
	}); err != nil {
		return nil, err
	}
	h.audit(ctx, sessionOf(ctx).AdminID, "node.outbound.import", "node", strconv.FormatInt(n.ID, 10), map[string]any{"name": name})
	h.d.Changes.SlotsChanged()
	n, err = h.nodeOr404(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	v, err := h.nodeNetworkView(ctx, n)
	if err != nil {
		return nil, err
	}
	return &nodeNetworkOutput{Body: v}, nil
}

type geoOutput struct {
	Body nodeapi.GeoStatus
}

func (h *handlers) nodeGeo(ctx context.Context, in *userIDInput) (*geoOutput, error) {
	n, err := h.nodeOr404(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if h.d.Nodes == nil {
		return nil, huma.Error409Conflict("nodes_disabled")
	}
	st, err := h.d.Nodes.GeoStatus(ctx, n.ID)
	if err != nil {
		if errors.Is(err, nodeapi.ErrUnavailable) {
			return nil, huma.Error409Conflict("nodes_disabled")
		}
		return nil, err
	}
	if st.Files == nil {
		st.Files = []nodeapi.GeoFile{}
	}
	return &geoOutput{Body: st}, nil
}

func (h *handlers) updateNodeGeo(ctx context.Context, in *userIDInput) (*geoOutput, error) {
	n, err := h.nodeOr404(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if h.d.Nodes == nil {
		return nil, huma.Error409Conflict("nodes_disabled")
	}
	st, err := h.d.Nodes.UpdateGeo(ctx, n.ID)
	if err != nil {
		if errors.Is(err, nodeapi.ErrUnavailable) {
			return nil, huma.Error409Conflict("nodes_disabled")
		}
		return nil, err
	}
	h.audit(ctx, sessionOf(ctx).AdminID, "node.geo.update", "node", strconv.FormatInt(n.ID, 10), nil)
	// Fresh files, fresh matchers: the node re-reads its rules on the new state.
	h.d.Changes.SlotsChanged()
	if st.Files == nil {
		st.Files = []nodeapi.GeoFile{}
	}
	return &geoOutput{Body: st}, nil
}
