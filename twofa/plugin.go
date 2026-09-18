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
	ctx  *plugin.Context
	host plugin.Host
	key  []byte
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
		&TwoFAAccount{},
		&TwoFAAuditLog{},
	}
}

func (p *Plugin) Mount(mux plugin.Mux, ctx *plugin.Context) {
	p.ctx = ctx
	p.host = plugin.EnsureHost(ctx)
	p.key = GetVaultKey("")

	// Register cross-plugin machine contract
	if ctx != nil && ctx.Provide != nil {
		ctx.Provide(ServiceName, Provider(p))
	}

	// Register health check provider if SPI is available
	if ctx != nil && ctx.Provide != nil {
		ctx.Provide(plugin.ServiceHealthProvider, plugin.HealthProvider(p))
	}

	guard := func(h http.Handler) http.Handler {
		if ctx != nil && ctx.Guard != nil {
			return ctx.Guard(h)
		}
		return h
	}

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
		{ID: "twofa", Label: "2FA Vault", Path: "/twofa", Icon: "🔐", Category: "Workspace"},
	}
}

func (*Plugin) HelpDocsFS() fs.FS {
	return docsFS
}

func (p *Plugin) Check(ctx context.Context) plugin.HealthResult {
	if p.ctx == nil || p.ctx.DB == nil {
		return plugin.HealthResult{
			Status:  plugin.HealthError,
			Message: "database connection is uninitialized",
		}
	}
	var count int64
	if err := p.ctx.DB.WithContext(ctx).Model(&TwoFAAccount{}).Count(&count).Error; err != nil {
		return plugin.HealthResult{
			Status:  plugin.HealthError,
			Message: fmt.Sprintf("failed to query accounts: %v", err),
		}
	}
	return plugin.HealthResult{
		Status:  plugin.HealthOK,
		Message: "2FA vault operational",
		Metrics: map[string]any{
			"total_accounts": count,
		},
	}
}

func (p *Plugin) Metadata() plugin.HealthMeta {
	return plugin.HealthMeta{
		Category: plugin.CategoryDatabase,
		Unit:     plugin.UnitCount,
	}
}

// GetCode implements the Provider contract for cross-plugin retrieval.
func (p *Plugin) GetCode(ctx context.Context, orgID uint, accountName string) (string, int, error) {
	if p.ctx == nil || p.ctx.DB == nil {
		return "", 0, errors.New("database not available")
	}

	var acc TwoFAAccount
	name := strings.TrimSpace(accountName)
	err := p.ctx.DB.WithContext(ctx).
		Where("org_id = ? AND (LOWER(name) = LOWER(?) OR LOWER(issuer) = LOWER(?))", orgID, name, name).
		First(&acc).Error
	if err != nil {
		return "", 0, fmt.Errorf("account %q not found in workspace %d", accountName, orgID)
	}

	secret, err := DecryptSecret(acc.EncryptedSecret, p.key)
	if err != nil {
		return "", 0, fmt.Errorf("failed to decrypt secret: %w", err)
	}

	now := time.Now()
	code, err := GenerateCode(secret, now, acc.Period, acc.Digits, acc.Algorithm)
	if err != nil {
		return "", 0, err
	}

	return code, SecondsRemaining(now, acc.Period), nil
}

// VerifyCode implements the Provider contract for cross-plugin verification.
func (p *Plugin) VerifyCode(ctx context.Context, orgID uint, accountName string, code string) (bool, error) {
	if p.ctx == nil || p.ctx.DB == nil {
		return false, errors.New("database not available")
	}

	var acc TwoFAAccount
	name := strings.TrimSpace(accountName)
	err := p.ctx.DB.WithContext(ctx).
		Where("org_id = ? AND (LOWER(name) = LOWER(?) OR LOWER(issuer) = LOWER(?))", orgID, name, name).
		First(&acc).Error
	if err != nil {
		return false, fmt.Errorf("account %q not found in workspace %d", accountName, orgID)
	}

	secret, err := DecryptSecret(acc.EncryptedSecret, p.key)
	if err != nil {
		return false, fmt.Errorf("failed to decrypt secret: %w", err)
	}

	return VerifyCode(secret, code, time.Now(), acc.Period, acc.Digits, acc.Algorithm, 1), nil
}

// Compile-time assertions for contracts
var (
	_ plugin.Plugin         = (*Plugin)(nil)
	_ plugin.Describer      = (*Plugin)(nil)
	_ plugin.MenuProvider   = (*Plugin)(nil)
	_ plugin.HelpDocsFS     = (*Plugin)(nil)
	_ plugin.HealthProvider = (*Plugin)(nil)
	_ plugin.MCPProvider    = (*Plugin)(nil)
	_ Provider              = (*Plugin)(nil)
)
