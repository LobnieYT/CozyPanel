package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"cozy/internal/nodeapi"
	"cozy/internal/nodetls"
	"cozy/internal/panel/acme"
	"cozy/internal/panel/api"
	"cozy/internal/panel/auth"
	"cozy/internal/panel/autotune"
	"cozy/internal/panel/dnscheck"
	"cozy/internal/panel/domain"
	"cozy/internal/panel/netcfg"
	"cozy/internal/panel/nodesync"
	"cozy/internal/panel/server"
	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
	"cozy/internal/panel/store/db"
	"cozy/internal/panel/subs"
	"cozy/internal/panel/tgbot"
	"cozy/internal/panel/tlscert"
	"cozy/internal/panel/updates"
	"cozy/internal/panel/warp"
)

// Panel is the fully wired HTTP side of the panel, without the listener.
type Panel struct {
	Handler   http.Handler
	Settings  *settings.Settings
	Nodes     *nodesync.Manager
	Tuner     *autotune.Tuner // nil without nodes
	Telegram  *tgbot.Bot
	Updates   *updates.Checker
	server    *server.Server
	spa       *server.SPA
	subPage   *server.SPA
	sessions  *auth.Sessions
	st        *store.Store
	devices   *domain.Devices
	ipLimit   *auth.Limiter
	userLimit *auth.Limiter
	now       func() time.Time
	log       *slog.Logger
}

type Options struct {
	Version    string
	Web        fs.FS
	TrustProxy bool
	Log        *slog.Logger
	Now        func() time.Time
	// Connect reaches a node; nil when the panel runs without nodes (tests, UI development).
	Connect nodesync.Connect
	// QUIC is a node's long-lived self-signed Hysteria2/TUIC certificate and its pin,
	// which subscription links carry.
	QUIC func(n db.Node) (*nodeapi.TLSFiles, string, error)
	// PanelCert is the client certificate remote nodes pin; join keys carry its hash.
	PanelCert func() (nodetls.Pair, error)
	// NodeCerts keeps the nodes' own certificates; nil: nodes have none.
	NodeCerts *tlscert.NodeStore
	// Certs manages the panel's public certificate; nil in development.
	Certs *acme.Manager
	// Autotune are the automatic moves' timings; zero means autotune.DefaultOptions.
	Autotune autotune.Options
	// TelegramAPI is the Bot API; "" is Telegram's.
	TelegramAPI string
	// SubPort moves the subscription port (0: none) and SubPortError says why the saved
	// one is not served; nil where the panel runs no server (tests, the CLI).
	SubPort      func(port int) error
	SubPortError func() string
	// DataDir is where the host updater and the panel meet (update/); "" turns that off.
	DataDir string
	// Releases fetches the newest release; nil never checks.
	Releases updates.Source
	// WarpAPI is Cloudflare's WARP client API; "" is the real one.
	WarpAPI string
	// DNS checks new domains against public DNS; nil leaves them unchecked.
	DNS *dnscheck.Checker
	// HSTS says whether browsers are told to keep to HTTPS: for a panel that serves TLS
	// itself, while its certificate is trusted (see server.SetHSTS). nil: never.
	HSTS func() bool
	// Resolve looks up the names the panel is told to dial (REALITY targets); nil is the
	// system's resolver, tests set their own.
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
}

type noChanges struct{}

func (noChanges) PoliciesChanged() {}
func (noChanges) SlotsChanged()    {}

func NewPanel(st *store.Store, o Options) (*Panel, error) {
	set := settings.New(st.Q)
	p := &Panel{
		Settings:  set,
		sessions:  auth.NewSessions(st.Q, o.Now, o.Log),
		ipLimit:   auth.NewLimiter(10, 10*time.Minute, 15*time.Minute, 24*time.Hour),
		userLimit: auth.NewLimiter(30, 10*time.Minute, 15*time.Minute, 24*time.Hour),
		now:       o.Now,
		log:       o.Log,
	}
	pool := domain.NewPool(st, o.Now)
	var changes domain.Changes = noChanges{}
	deps := api.Deps{
		Version: o.Version, Store: st, Settings: set, Sessions: p.sessions,
		IPLimit: p.ipLimit, UserLimit: p.userLimit, TOTP: auth.NewTOTPGuard(),
		TrustProxy: o.TrustProxy, Log: o.Log, Now: o.Now, Pool: pool,
	}
	if o.Connect != nil {
		p.Nodes = nodesync.NewManager(st, set, pool, o.Connect, o.Log, o.Now)
		changes = p.Nodes
		deps.Online = p.Nodes.Online
		deps.Nodes = p.Nodes
		tune := o.Autotune
		if tune == (autotune.Options{}) {
			tune = autotune.DefaultOptions()
		}
		p.Tuner = autotune.New(st, set, p.Nodes, p.Nodes, o.Log, o.Now, tune)
		deps.Tuner = p.Tuner
	}
	deps.PanelCert = o.PanelCert
	deps.SubPort, deps.SubPortError = o.SubPort, o.SubPortError
	deps.Changes = changes
	deps.Users = domain.NewUsers(st, pool, changes, o.Now)
	// The nodes try a new listener before it is saved, when the panel runs them.
	var dryRun domain.DryRun
	if p.Nodes != nil {
		dryRun = p.Nodes
	}
	deps.Inbounds = domain.NewInbounds(st, dryRun, o.Now)
	// A REALITY target given by name is looked up when it is saved: the node dials it past the
	// rules that fence its users in.
	deps.Resolve = o.Resolve
	if deps.Resolve == nil {
		deps.Resolve = domain.SystemResolve
	}
	deps.Inbounds.SetResolver(deps.Resolve)
	deps.Devices = domain.NewDevices(st, pool, changes, o.Now)
	p.st, p.devices = st, deps.Devices
	deps.Packages = domain.NewPackages(st, o.Now)
	if o.Certs != nil {
		deps.Cert, deps.RenewCert = o.Certs.Status, o.Certs.Renew
		deps.SetCert, deps.ClearCert = o.Certs.SetCustom, o.Certs.ClearCustom
	}
	deps.NodeCerts = o.NodeCerts
	deps.ForgetNode = forgetNodeFiles(o)
	subBase := func(ctx context.Context) string {
		ep, err := set.SubEndpoint(ctx)
		if err != nil || ep.Host == "" {
			return ""
		}
		paths, err := set.Paths(ctx)
		if err != nil {
			return ""
		}
		return "https://" + net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port)) + "/" + paths.Sub
	}
	deps.DNS = o.DNS
	deps.SubBase = subBase
	// The bot may reach Telegram through a node when the panel's server cannot.
	var tunnel func(ctx context.Context, nodeID int64, addr string) (net.Conn, error)
	if p.Nodes != nil {
		tunnel = p.Nodes.Tunnel
	}
	p.Telegram = tgbot.New(tgbot.Deps{Store: st, Settings: set, Devices: deps.Devices, SubBase: subBase, API: o.TelegramAPI, Log: o.Log, Now: o.Now,
		// Telegram apps refuse a Mini App on a self-signed certificate.
		MiniApp: func() bool { return o.Certs != nil && o.Certs.Status().Kind == "letsencrypt" }, Tunnel: tunnel})
	deps.Telegram = p.Telegram
	p.Updates = updates.New(o.DataDir, o.Version, o.Releases, o.Log, o.Now)
	deps.Updates = p.Updates
	deps.Warp = warp.Client{API: o.WarpAPI}
	apiHandler, _, err := api.New(deps)
	if err != nil {
		return nil, err
	}
	p.spa, err = server.NewSPA(o.Web, "index.html")
	if err != nil {
		return nil, fmt.Errorf("web bundle: %w", err)
	}
	var subPageHandler http.Handler
	if sp, err := server.NewSPA(o.Web, "sub.html"); err == nil {
		p.subPage, subPageHandler = sp, sp
	}
	buildSubCfg := func(ctx context.Context) (subs.Config, error) {
		ep, err := set.Endpoint(ctx)
		if err != nil {
			return subs.Config{}, err
		}
		brand, _, err := settings.Get[string](ctx, set, settings.KeyBrand)
		if err != nil {
			return subs.Config{}, err
		}
		if brand == "" {
			brand = "Cozy"
		}
		support, _, err := settings.Get[string](ctx, set, settings.KeySupportURL)
		if err != nil {
			return subs.Config{}, err
		}
		lang, err := set.Lang(ctx)
		if err != nil {
			return subs.Config{}, err
		}
		var domainName, publicHost, routing, fingerprint, rules string
		var groups subs.Groups
		for key, dst := range map[string]*string{settings.KeyDomain: &domainName, settings.KeyPublicHost: &publicHost, settings.KeyGroupMain: &groups.Main, settings.KeyGroupAuto: &groups.Auto,
			settings.KeyRouting: &routing, settings.KeyFingerprint: &fingerprint, settings.KeyRules: &rules} {
			if *dst, err = set.String(ctx, key); err != nil {
				return subs.Config{}, err
			}
		}
		cfg := subs.Config{Brand: brand, SupportURL: support, Groups: groups, Routing: subs.ParseRouting(routing), Fingerprint: fingerprint,
			Direct: []string{publicHost, domainName}, Lang: lang, Rules: subs.ServedRules(rules, groups.WithDefaults(lang))}
		if dnsRaw, err := set.String(ctx, settings.KeySubDNS); err != nil {
			return subs.Config{}, err
		} else if cfg.DNS, err = netcfg.ParseSubDNS(dnsRaw); err != nil {
			return subs.Config{}, err
		}
		if cfg.Binding, err = set.On(ctx, settings.DeviceBinding); err != nil {
			return subs.Config{}, err
		}
		if cfg.RequireHWID, err = set.On(ctx, settings.RequireHWID); err != nil {
			return subs.Config{}, err
		}
		nodes, err := st.Q.ListNodes(ctx)
		if err != nil {
			return subs.Config{}, err
		}
		for _, n := range nodes {
			if n.Enabled == 0 {
				continue
			}
			sn := subs.Node{ID: n.ID, Name: n.Name, Endpoint: subs.Endpoint{Host: ep.Host, SNI: domainName}}
			if n.Address != "" {
				sn.Endpoint = subs.Endpoint{Host: domain.NodeHost(n), SNI: n.Domain}
				cfg.Direct = append(cfg.Direct, n.PublicHost, n.Domain)
			}
			if o.QUIC != nil {
				if _, pin, err := o.QUIC(n); err == nil {
					sn.Endpoint.PinSHA256 = pin
				}
			}
			cfg.Nodes = append(cfg.Nodes, sn)
		}
		return cfg, nil
	}
	// A subscription is fetched by every app of every user every hour or so, and building
	// its config reads some twenty settings and every node and takes a node's certificate
	// from disk. It is kept for a few seconds, and dropped at once when this process changes
	// a setting, a node or a node's certificate.
	cache := &configCache{now: o.Now, gen: func() uint64 {
		g := settings.Generation()
		if p.Nodes != nil {
			g += p.Nodes.Generation()
		}
		if o.NodeCerts != nil {
			g += o.NodeCerts.Generation()
		}
		return g
	}}
	subCfg := func(ctx context.Context) (subs.Config, error) { return cache.get(ctx, buildSubCfg) }
	subHandler := subs.NewHandler(st, subCfg, subPageHandler, o.Now, deps.Devices, o.TrustProxy)
	subHandler.SetLogger(o.Log)
	subHandler.SetTelegram(p.Telegram)

	adminMux := http.NewServeMux()
	adminMux.Handle("/api/", apiHandler)
	adminMux.Handle("/", p.spa)
	p.server = server.New(adminMux, subHandler)
	p.server.SetHSTS(o.HSTS)
	p.Handler = p.server
	return p, nil
}

// SubOnly is what the subscription port serves: the subscription path alone.
func (p *Panel) SubOnly() http.Handler { return p.server.SubOnly() }

// Apply loads the secret paths into the router and the default language into the pages.
func (p *Panel) Apply(ctx context.Context) (settings.Paths, error) {
	paths, err := p.Settings.Paths(ctx)
	if err != nil {
		return paths, err
	}
	lang, err := p.Settings.Lang(ctx)
	if err != nil {
		return paths, err
	}
	p.server.SetPaths(paths)
	p.spa.SetPrefix(paths.Admin)
	p.spa.SetLang(lang)
	if p.subPage != nil {
		p.subPage.SetPrefix(paths.Sub)
		p.subPage.SetLang(lang)
	}
	return paths, nil
}

// Run keeps paths and the language in sync with the DB (the CLI and the settings page
// edit them), drives the node syncer and cleans up expired state. It returns once ctx is
// done and every one of its workers has stopped: the caller closes the database after it.
func (p *Panel) Run(ctx context.Context) {
	// The host reads the switch from a file; the setting is what the admin chose.
	if auto, err := p.Settings.On(ctx, settings.AutoUpdate); err == nil {
		if err := p.Updates.SetAuto(auto); err != nil && !errors.Is(err, updates.ErrUnavailable) {
			p.log.Error("update policy", "err", err)
		}
	}
	var workers []func(context.Context)
	if p.Nodes != nil {
		workers = append(workers, p.Nodes.Run)
	}
	if p.Tuner != nil {
		workers = append(workers, p.Tuner.Run)
	}
	workers = append(workers, p.Telegram.Run, p.Updates.Run,
		func(ctx context.Context) {
			every(ctx, 5*time.Second, func() {
				if _, err := p.Apply(ctx); err != nil {
					p.log.Error("reload settings", "err", err)
				}
			})
		},
		func(ctx context.Context) {
			every(ctx, 10*time.Minute, func() {
				if err := p.sessions.Cleanup(ctx); err != nil {
					p.log.Error("session cleanup", "err", err)
				}
				p.ipLimit.Sweep(p.now())
				p.userLimit.Sweep(p.now())
				p.maintain(ctx)
			})
		})
	runAll(ctx, workers...)
}

// runAll runs the workers until ctx is done and waits for every one to return, so that
// nothing is still using what the caller shuts down after it (the database).
func runAll(ctx context.Context, workers ...func(context.Context)) {
	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Go(func() { w(ctx) })
	}
	wg.Wait()
}

// configCacheTTL: the longest a changed setting is not seen, when the change was made by
// another process (the server CLI); this process's own changes drop the cache at once.
const configCacheTTL = 10 * time.Second

// configCache keeps the subscription's config between requests. Errors are not kept.
type configCache struct {
	now func() time.Time
	gen func() uint64

	mu  sync.Mutex
	at  time.Time
	g   uint64
	cfg subs.Config
	ok  bool
}

func (c *configCache) get(ctx context.Context, build func(context.Context) (subs.Config, error)) (subs.Config, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now, g := c.now(), c.gen()
	if c.ok && g == c.g && now.Sub(c.at) < configCacheTTL && !now.Before(c.at) {
		return c.cfg, nil
	}
	cfg, err := build(ctx)
	if err != nil {
		return subs.Config{}, err
	}
	c.cfg, c.g, c.at, c.ok = cfg, g, now, true
	return cfg, nil
}

// forgetNodeFiles removes what the panel keeps on disk for a node id: its own certificate
// (Options.NodeCerts) and the self-signed pair its QUIC protocols use, <data>/tls/nodes/<id>.
func forgetNodeFiles(o Options) func(id int64) error {
	return func(id int64) error {
		var errs []error
		if o.NodeCerts != nil {
			errs = append(errs, o.NodeCerts.Clear(id))
		}
		if o.DataDir != "" {
			errs = append(errs, os.RemoveAll(filepath.Join(o.DataDir, "tls", "nodes", strconv.FormatInt(id, 10))))
		}
		return errors.Join(errs...)
	}
}

func every(ctx context.Context, d time.Duration, fn func()) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}
