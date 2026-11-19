// Package pt manages private-tracker (PT) accounts: one-click login to refresh
// cookies, daily sign-in inside a per-site window, and account statistics
// (level, traffic, ratio, bonus, warnings).
//
// It is deliberately browser-free. Sites that need JavaScript challenges or a
// captcha are reported as "needs manual action" with a paste-a-cookie path,
// instead of pretending the automation succeeded.
package pt

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
	"github.com/octarq-org/octarq/server/plugin/safehttp"
	"gorm.io/gorm"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

//go:embed docs
var docsFS embed.FS

// userAgent is the default UA. A tracker's own UserAgent overrides it; the
// point of showing it in the UI is that operators can match the browser they
// registered with, which several sites bind cookies to.
const userAgent = "Mozilla/5.0 (compatible; octarq-pt/1.0)"

// Plugin is the PT account manager composed into an Octarq build via
// app.Use(pt.New()).
type Plugin struct {
	mu  sync.RWMutex
	ctx *plugin.Context
	// host is captured only from a real HTTP mount. The MCP composition mounts
	// plugins with a no-op host, and adopting that stub would silently disable
	// the credential vault and tenant scoping.
	host     plugin.Host
	db       *gorm.DB
	vaultSvc plugin.VaultService

	registryOnce sync.Once
	registryVal  *adapter.Registry

	limiterOnce sync.Once
	limiterVal  *limiter

	// emit is the host notification sink, captured at mount.
	emit func(context.Context, plugin.NotificationPayload) error

	// newClient is injectable so tests can reach a loopback fixture server.
	// Production defaults to safehttp, which blocks private destinations
	// because the tracker URL is user-supplied.
	newClient func(time.Duration) *http.Client
	now       func() time.Time

	cronRegister  func(name, spec string, handler func(context.Context) error) error
	cronAvailable bool
}

// New returns a ready plugin. The zero value also works, so a build can write
// &pt.Plugin{}.
func New() *Plugin { return &Plugin{} }

// Name is the stable plugin identifier and must match the frontend
// UIPlugin.name.
func (*Plugin) Name() string { return "pt" }

// Describe reports plugin metadata for the plugin manager.
func (*Plugin) Describe() plugin.Info {
	return plugin.Info{
		Title:       "PT Accounts",
		Description: "Manage private-tracker accounts: refresh logins, sign in daily, and track upload, ratio, bonus, and warnings.",
		Category:    plugin.CategoryUtilities,
		Tags:        []string{"pt", "tracker", "signin"},
	}
}

// Models returns the tables this plugin owns. Core migrates the union of every
// plugin's models once at boot; this package never calls AutoMigrate.
func (*Plugin) Models() []any {
	return []any{
		&PTTracker{},
		&PTSnapshot{},
		&PTSigninResult{},
	}
}

// Menus places the page in the sidebar. The menu is declared here and only
// here; the frontend contributes routes, not navigation.
func (*Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{ID: "pt", Label: "PT Accounts", Path: "/personal/pt", Icon: "hard-drive", Category: "Accounts", Area: "personal", Order: 40},
	}
}

// HelpDocsFS serves the help center pages.
func (*Plugin) HelpDocsFS() fs.FS { return docsFS }

// Mount registers the plugin's HTTP surface.
//
// The MCP composition mounts every plugin a second time with a nil mux and a
// no-op host; this method records that DB handle but returns before adopting
// the stub host, so the credential vault keeps working.
func (p *Plugin) Mount(mux plugin.Mux, ctx *plugin.Context) {
	if ctx == nil {
		return
	}
	p.mu.Lock()
	if ctx.DB != nil {
		p.db = ctx.DB
	}
	if ctx.RegisterCron != nil {
		p.cronRegister = ctx.RegisterCron
		p.cronAvailable = true
	}
	if ctx.Emit != nil {
		p.emit = ctx.Emit
	}
	p.mu.Unlock()

	if mux == nil || ctx.Guard == nil {
		return
	}

	p.mu.Lock()
	p.ctx = ctx
	if ctx.Host != nil {
		p.host = ctx.Host
	}
	p.mu.Unlock()
	p.registerRoutes(ctx)
}

// Validate refuses to start without a credential vault. Storing tracker
// account secrets unencrypted is not an acceptable degraded mode.
func (p *Plugin) Validate(ctx context.Context, host plugin.Host) error {
	if _, err := p.vault(); err != nil {
		return errors.New("pt: credential vault unavailable (ServiceVault not provided); refusing to start")
	}
	return nil
}

// Start registers the scheduler. A host without cron keeps manual actions
// working, so this is a warning rather than a boot failure.
func (p *Plugin) Start(ctx context.Context) {
	p.mu.RLock()
	reg := p.cronRegister
	p.mu.RUnlock()
	if reg == nil {
		return
	}
	if err := reg(cronTickJob, cronTickSpec, p.runTick); err != nil {
		// Losing the schedule degrades to manual-only; the API reports it.
		p.mu.Lock()
		p.cronAvailable = false
		p.mu.Unlock()
		return
	}
	if err := reg(cronRetentionJob, cronRetentionSpec, p.runRetention); err != nil {
		// Retention is housekeeping; failing to register it is not fatal.
		_ = err
	}
}

// Stop is a no-op: every job runs to completion inside its context.
func (*Plugin) Stop(context.Context) error { return nil }

// registry returns the compiled-in site dialects.
func (p *Plugin) registry() *adapter.Registry {
	p.registryOnce.Do(func() {
		p.registryVal = buildRegistry()
	})
	return p.registryVal
}

// clock returns the current time, overridable in tests.
func (p *Plugin) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// client builds the outbound HTTP client for one tracker.
func (p *Plugin) client(timeout time.Duration) *http.Client {
	if p.newClient != nil {
		return p.newClient(timeout)
	}
	return safehttp.NewClient(timeout)
}

// hostRef returns the captured host, or nil before a real mount.
func (p *Plugin) hostRef() plugin.Host {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.host
}

// dbRef returns the shared DB handle.
func (p *Plugin) dbRef() *gorm.DB {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.db
}

// schedulerAvailable reports whether cron registration succeeded.
func (p *Plugin) schedulerAvailable() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cronAvailable
}

var (
	_ plugin.Plugin       = (*Plugin)(nil)
	_ plugin.Validator    = (*Plugin)(nil)
	_ plugin.Starter      = (*Plugin)(nil)
	_ plugin.Stopper      = (*Plugin)(nil)
	_ plugin.Describer    = (*Plugin)(nil)
	_ plugin.MenuProvider = (*Plugin)(nil)
	_ plugin.HelpDocsFS   = (*Plugin)(nil)
)

// errVaultUnavailable is returned by every credential path when VaultService is
// absent. It is a configuration fault, not a user error.
var errVaultUnavailable = errors.New("pt: credential vault unavailable")

func vaultMissing(op string) error {
	return fmt.Errorf("%w (%s)", errVaultUnavailable, op)
}
