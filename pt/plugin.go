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

const userAgent = "Mozilla/5.0 (compatible; octarq-pt/1.0)"

type Plugin struct {
	mu       sync.RWMutex
	ctx      *plugin.Context
	host     plugin.Host
	db       *gorm.DB
	vaultSvc plugin.VaultService

	registryOnce sync.Once
	registryVal  *adapter.Registry

	limiterOnce sync.Once
	limiterVal  *limiter

	emit func(context.Context, plugin.NotificationPayload) error

	newClient func(time.Duration) *http.Client
	now       func() time.Time

	cronRegister  func(name, spec string, handler func(context.Context) error) error
	cronAvailable bool
}

func New() *Plugin { return &Plugin{} }

func (*Plugin) Name() string { return "pt" }

func (*Plugin) Describe() plugin.Info {
	return plugin.Info{
		Title:            "PT Accounts",
		Description:      "Manage private-tracker accounts: refresh logins, sign in daily, and track upload, ratio, bonus, and warnings.",
		Category:         plugin.CategoryUtilities,
		Tags:             []string{"pt", "tracker", "signin"},
		EnabledByDefault: true,
	}
}

func (*Plugin) Models() []any {
	return []any{
		&PTTracker{},
		&PTSnapshot{},
		&PTSigninResult{},
	}
}

func (*Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{ID: "pt", Label: "PT Accounts", Path: "/personal/pt", Icon: "hard-drive", Category: "Accounts", Area: "personal", Order: 40},
	}
}

func (*Plugin) HelpDocsFS() fs.FS { return docsFS }

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

func (p *Plugin) Validate(ctx context.Context, host plugin.Host) error {
	if _, err := p.vault(); err != nil {
		return errors.New("pt: credential vault unavailable (ServiceVault not provided); refusing to start")
	}
	return nil
}

func (p *Plugin) Start(ctx context.Context) {
	p.mu.RLock()
	reg := p.cronRegister
	p.mu.RUnlock()
	if reg == nil {
		return
	}
	if err := reg(cronTickJob, cronTickSpec, p.runTick); err != nil {
		p.mu.Lock()
		p.cronAvailable = false
		p.mu.Unlock()
		return
	}
	if err := reg(cronRetentionJob, cronRetentionSpec, p.runRetention); err != nil {
		_ = err
	}
}

func (*Plugin) Stop(context.Context) error { return nil }

func (p *Plugin) registry() *adapter.Registry {
	p.registryOnce.Do(func() {
		p.registryVal = buildRegistry()
	})
	return p.registryVal
}

func (p *Plugin) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *Plugin) client(timeout time.Duration) *http.Client {
	if p.newClient != nil {
		return p.newClient(timeout)
	}
	return safehttp.NewClient(timeout)
}

func (p *Plugin) hostRef() plugin.Host {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.host
}

func (p *Plugin) dbRef() *gorm.DB {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.db
}

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

var errVaultUnavailable = errors.New("pt: credential vault unavailable")

func vaultMissing(op string) error {
	return fmt.Errorf("%w (%s)", errVaultUnavailable, op)
}
