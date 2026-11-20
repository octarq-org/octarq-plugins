package twofa

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

//go:embed docs
var docsFS embed.FS

type Plugin struct {
	ctx      *plugin.Context
	host     plugin.Host
	vaultSvc plugin.VaultService
	db       *gorm.DB
}

func (*Plugin) Name() string { return "twofa" }

func (*Plugin) Describe() plugin.Info {
	return plugin.Info{
		Title:            "2FA Vault",
		Description:      "Personal TOTP two-factor authentication vault with live code generation, encrypted storage, opt-in workspace sharing, and AI agent MCP tools.",
		EnabledByDefault: true,
	}
}

func (*Plugin) Models() []any {
	return []any{&TwoFAShare{}}
}

func (p *Plugin) Mount(mux plugin.Mux, ctx *plugin.Context) {
	p.ctx = ctx
	p.host = plugin.EnsureHost(ctx)
	if ctx != nil {
		p.db = ctx.DB
	}

	if ctx != nil && ctx.Provide != nil {
		ctx.Provide(ServiceName, Provider(p))
	}

	if ctx != nil && ctx.Provide != nil {
		ctx.Provide(plugin.ServiceHealthProvider, plugin.HealthProvider(p))
	}

	if mux == nil || ctx.Guard == nil {
		return
	}
	guard := func(h http.Handler) http.Handler { return ctx.Guard(h) }

	const base = "/api/twofa"
	mux.Handle("GET "+base+"/accounts", guard(http.HandlerFunc(p.listAccounts)))
	mux.Handle("POST "+base+"/accounts", guard(http.HandlerFunc(p.createAccount)))
	mux.Handle("GET "+base+"/accounts/{id}", guard(http.HandlerFunc(p.getAccount)))
	mux.Handle("PUT "+base+"/accounts/{id}", guard(http.HandlerFunc(p.updateAccount)))
	mux.Handle("DELETE "+base+"/accounts/{id}", guard(http.HandlerFunc(p.deleteAccount)))
	mux.Handle("POST "+base+"/accounts/{id}/pin", guard(http.HandlerFunc(p.togglePin)))
	mux.Handle("POST "+base+"/accounts/{id}/share", guard(http.HandlerFunc(p.setShare)))
	mux.Handle("POST "+base+"/accounts/{id}/code", guard(http.HandlerFunc(p.getCode)))
	mux.Handle("POST "+base+"/accounts/{id}/verify", guard(http.HandlerFunc(p.verifyCode)))
	mux.Handle("GET "+base+"/accounts/{id}/qr", guard(http.HandlerFunc(p.getQRCode)))
	mux.Handle("POST "+base+"/import", guard(http.HandlerFunc(p.importAccounts)))
	mux.Handle("GET "+base+"/export", guard(http.HandlerFunc(p.exportAccounts)))
}

func (*Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{
			ID: "twofa", Label: "2FA Vault", Path: "/personal/twofa", Icon: "shield",
			Category: "Accounts", Area: "personal", Order: 20,
		},
	}
}

func (*Plugin) HelpDocsFS() fs.FS {
	return docsFS
}

func (p *Plugin) vault() (plugin.VaultService, error) {
	if p.vaultSvc != nil {
		return p.vaultSvc, nil
	}
	if svc, ok := plugin.LookupAs[plugin.VaultService](p.ctx, plugin.ServiceVault); ok && svc != nil {
		return svc, nil
	}
	return nil, plugin.ErrVaultUnavailable
}

func (p *Plugin) Check(ctx context.Context) plugin.HealthResult {
	if _, err := p.vault(); err != nil {
		return plugin.HealthResult{
			Status:  plugin.HealthError,
			Message: "credential vault service is unavailable",
		}
	}
	if p.ctx == nil || p.ctx.DB == nil {
		return plugin.HealthResult{
			Status:  plugin.HealthError,
			Message: "database connection is uninitialized",
		}
	}
	sqlDB, err := p.ctx.DB.DB()
	if err != nil {
		return plugin.HealthResult{
			Status:  plugin.HealthError,
			Message: fmt.Sprintf("database handle error: %v", err),
		}
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return plugin.HealthResult{
			Status:  plugin.HealthError,
			Message: fmt.Sprintf("database ping failed: %v", err),
		}
	}
	return plugin.HealthResult{
		Status:  plugin.HealthOK,
		Message: "2FA vault operational",
	}
}

func (p *Plugin) Metadata() plugin.HealthMeta {
	return plugin.HealthMeta{
		Category: plugin.CategoryDatabase,
		Unit:     plugin.UnitCount,
	}
}

func (p *Plugin) Validate(ctx context.Context, host plugin.Host) error {
	if host == nil {
		return errors.New("twofa: host runtime is required")
	}
	if _, err := p.vault(); err != nil {
		return errors.New("twofa: VaultService unavailable (ServiceVault not provided)")
	}
	return nil
}

func (p *Plugin) GetCode(ctx context.Context, orgID uint, accountName string) (string, int, error) {
	if orgID == 0 {
		return "", 0, errors.New("unauthorized org")
	}
	vs, err := p.vault()
	if err != nil {
		return "", 0, err
	}
	item, err := p.resolveSharedItem(ctx, vs, orgID, accountName)
	if err != nil {
		return "", 0, err
	}
	return vs.ComputeTOTP(ctx, orgID, item.ID)
}

func (p *Plugin) VerifyCode(ctx context.Context, orgID uint, accountName string, code string) (bool, error) {
	if orgID == 0 {
		return false, errors.New("unauthorized org")
	}
	vs, err := p.vault()
	if err != nil {
		return false, err
	}
	item, err := p.resolveSharedItem(ctx, vs, orgID, accountName)
	if err != nil {
		return false, err
	}
	sec, err := vs.GetSecret(ctx, orgID, item.ID)
	if err != nil {
		return false, fmt.Errorf("failed to resolve secret: %w", err)
	}
	acc := metaToAccount(*item)
	secret := extractSecretFromPayload(sec.Payload)
	return VerifyCode(secret, code, time.Now(), acc.Period, acc.Digits, acc.Algorithm, 1), nil
}

var (
	_ plugin.Plugin         = (*Plugin)(nil)
	_ plugin.Validator      = (*Plugin)(nil)
	_ plugin.Describer      = (*Plugin)(nil)
	_ plugin.MenuProvider   = (*Plugin)(nil)
	_ plugin.HelpDocsFS     = (*Plugin)(nil)
	_ plugin.HealthProvider = (*Plugin)(nil)
	_ plugin.MCPProvider    = (*Plugin)(nil)
	_ Provider              = (*Plugin)(nil)
)
