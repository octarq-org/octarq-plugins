// Package twofa provides an enterprise-grade 2FA / TOTP Authenticator Vault
// for Octarq. It stores two-factor authentication seeds encrypted at rest,
// generates live TOTP codes, and exposes MCP tools for AI agents.
package twofa

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
)

//go:embed docs
var docsFS embed.FS

// Plugin is the 2FA Vault plugin struct composed into Octarq via app.Use(&twofa.Plugin{}).
type Plugin struct {
	ctx      *plugin.Context
	host     plugin.Host
	vaultSvc plugin.VaultService
}

func (*Plugin) Name() string { return "twofa" }

func (*Plugin) Describe() plugin.Info {
	return plugin.Info{
		Title:       "2FA Vault",
		Description: "Centralized TOTP two-factor authentication manager for team & infrastructure accounts with live code generation, encrypted storage, and AI agent MCP tools.",
	}
}

func (*Plugin) Models() []any {
	return []any{
		&TwoFAAuditLog{},
	}
}

func (p *Plugin) Mount(mux plugin.Mux, ctx *plugin.Context) {
	p.ctx = ctx
	p.host = plugin.EnsureHost(ctx)

	// Register cross-plugin machine contract
	if ctx != nil && ctx.Provide != nil {
		ctx.Provide(ServiceName, Provider(p))
	}

	// Register health check provider if SPI is available
	if ctx != nil && ctx.Provide != nil {
		ctx.Provide(plugin.ServiceHealthProvider, plugin.HealthProvider(p))
	}

	// The MCP composition path mounts this plugin with no HTTP surface (nil
	// mux, nil Guard) because it only wants the MCP tools. Skip the routes
	// there; never serve an unguarded one.
	if mux == nil || ctx.Guard == nil {
		return
	}
	guard := func(h http.Handler) http.Handler { return ctx.Guard(h) }

	mux.Handle("GET /api/twofa/accounts", guard(http.HandlerFunc(p.listAccounts)))
	mux.Handle("POST /api/twofa/accounts", guard(http.HandlerFunc(p.createAccount)))
	mux.Handle("GET /api/twofa/accounts/{id}", guard(http.HandlerFunc(p.getAccount)))
	mux.Handle("PUT /api/twofa/accounts/{id}", guard(http.HandlerFunc(p.updateAccount)))
	mux.Handle("DELETE /api/twofa/accounts/{id}", guard(http.HandlerFunc(p.deleteAccount)))
	mux.Handle("POST /api/twofa/accounts/{id}/pin", guard(http.HandlerFunc(p.togglePin)))
	mux.Handle("POST /api/twofa/accounts/{id}/code", guard(http.HandlerFunc(p.getCode)))
	mux.Handle("POST /api/twofa/accounts/{id}/verify", guard(http.HandlerFunc(p.verifyCode)))
	mux.Handle("GET /api/twofa/accounts/{id}/qr", guard(http.HandlerFunc(p.getQRCode)))
	mux.Handle("POST /api/twofa/import", guard(http.HandlerFunc(p.importAccounts)))
	mux.Handle("GET /api/twofa/export", guard(http.HandlerFunc(p.exportAccounts)))
	mux.Handle("GET /api/twofa/logs", guard(http.HandlerFunc(p.listAuditLogs)))
}

func (*Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{ID: "twofa", Label: "2FA Vault", Path: "/twofa", Icon: "shield", Category: "Security"},
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
	var count int64
	if err := p.ctx.DB.WithContext(ctx).Model(&TwoFAAuditLog{}).Count(&count).Error; err != nil {
		return plugin.HealthResult{
			Status:  plugin.HealthError,
			Message: fmt.Sprintf("failed to query audit logs: %v", err),
		}
	}
	return plugin.HealthResult{
		Status:  plugin.HealthOK,
		Message: "2FA vault operational",
		Metrics: map[string]any{
			"audit_logs": count,
		},
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

func (p *Plugin) tenantDB(ctx context.Context, orgID uint) *plugin.TenantDB {
	if p.host != nil && orgID > 0 {
		tdb := p.host.TenantDB(orgID)
		if tdb != nil {
			return tdb.WithColumn("org_id").WithContext(ctx)
		}
	}
	return nil
}

// GetCode implements the Provider contract for cross-plugin retrieval.
func (p *Plugin) GetCode(ctx context.Context, orgID uint, accountName string) (string, int, error) {
	if orgID == 0 {
		return "", 0, errors.New("unauthorized org")
	}
	vs, err := p.vault()
	if err != nil {
		return "", 0, err
	}
	items, err := vs.ListItems(ctx, orgID, plugin.VaultFilter{
		Scope:    plugin.VaultScopeOrg,
		ItemType: "twofa_totp",
	})
	if err != nil {
		return "", 0, err
	}
	name := strings.TrimSpace(accountName)
	for _, it := range items {
		acc := metaToAccount(it)
		if strings.EqualFold(acc.Name, name) || strings.EqualFold(acc.Issuer, name) {
			return vs.ComputeTOTP(ctx, orgID, it.ID)
		}
	}
	return "", 0, fmt.Errorf("account %q not found in workspace %d", accountName, orgID)
}

// VerifyCode implements the Provider contract for cross-plugin verification.
func (p *Plugin) VerifyCode(ctx context.Context, orgID uint, accountName string, code string) (bool, error) {
	if orgID == 0 {
		return false, errors.New("unauthorized org")
	}
	vs, err := p.vault()
	if err != nil {
		return false, err
	}
	items, err := vs.ListItems(ctx, orgID, plugin.VaultFilter{
		Scope:    plugin.VaultScopeOrg,
		ItemType: "twofa_totp",
	})
	if err != nil {
		return false, err
	}
	name := strings.TrimSpace(accountName)
	for _, it := range items {
		acc := metaToAccount(it)
		if strings.EqualFold(acc.Name, name) || strings.EqualFold(acc.Issuer, name) {
			sec, sErr := vs.GetSecret(ctx, orgID, it.ID)
			if sErr != nil {
				return false, fmt.Errorf("failed to resolve secret: %w", sErr)
			}
			secret := extractSecretFromPayload(sec.Payload)
			return VerifyCode(secret, code, time.Now(), acc.Period, acc.Digits, acc.Algorithm, 1), nil
		}
	}
	return false, fmt.Errorf("account %q not found in workspace %d", accountName, orgID)
}

// Compile-time assertions for contracts
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
