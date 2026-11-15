package pt

import (
	"context"
	"net/http"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

func mcpTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:mcp_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&PTTracker{}, &PTCredential{}, &PTSnapshot{}, &PTSigninResult{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mountHTTP(t *testing.T, p *Plugin, host plugin.Host) {
	t.Helper()
	p.Mount(http.NewServeMux(), &plugin.Context{
		DB:    mcpTestDB(t),
		Guard: func(h http.Handler) http.Handler { return h },
		Host:  host,
	})
}

// The stdio MCP path validates against a no-op host after the server mount has
// already captured the real host. The captured vault is the one the tools use,
// so the plugin must boot there instead of stopping the whole MCP endpoint.
func TestValidateKeepsTheVaultAcrossTheMCPRemount(t *testing.T) {
	p := &Plugin{}
	mountHTTP(t, p, &plugin.TestHost{CryptoMock: newFakeVault()})

	// The MCP composition re-mounts with a nil mux and a stub host.
	p.Mount(nil, &plugin.Context{DB: mcpTestDB(t)})

	if err := p.Validate(context.Background(), plugin.EnsureHost(nil)); err != nil {
		t.Fatalf("MCP re-mount refused: %v", err)
	}
	if !p.vaultAvailable() {
		t.Fatal("the stub host replaced the real one")
	}
}

// A real server mount without a vault must refuse to start.
func TestValidateRefusesHTTPMountWithoutVault(t *testing.T) {
	p := &Plugin{}
	mountHTTP(t, p, &plugin.TestHost{})

	if err := p.Validate(context.Background(), &plugin.TestHost{}); err == nil {
		t.Fatal("a server mount without a vault started anyway")
	}
	if err := p.Validate(context.Background(), nil); err == nil {
		t.Fatal("a nil host started anyway")
	}
}

// A plugin that never saw a real host has nothing to serve, so it refuses.
func TestValidateRefusesWithoutAnyHost(t *testing.T) {
	p := &Plugin{}
	p.Mount(nil, &plugin.Context{DB: mcpTestDB(t)})

	if err := p.Validate(context.Background(), plugin.EnsureHost(nil)); err == nil {
		t.Fatal("a mount without a vault started anyway")
	}
	if p.vaultAvailable() {
		t.Fatal("a mount without a host reports a vault")
	}
}
