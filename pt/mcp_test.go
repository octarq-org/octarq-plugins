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

// The stdio MCP composition mounts every plugin with no HTTP surface and a
// no-op host. The plugin must boot there: it serves read tools and skips the
// tools that need the vault.
func TestValidateAcceptsMCPMountWithoutVault(t *testing.T) {
	p := &Plugin{}
	p.Mount(nil, &plugin.Context{DB: mcpTestDB(t)})

	if !p.mcpOnly {
		t.Fatal("a nil mux was not recorded as an MCP-only mount")
	}
	if p.vaultAvailable() {
		t.Fatal("the MCP mount reports a vault it does not have")
	}
	if err := p.Validate(context.Background(), plugin.EnsureHost(nil)); err != nil {
		t.Fatalf("MCP mount refused: %v", err)
	}
}

// A real server mount without a vault must still refuse to start.
func TestValidateRefusesHTTPMountWithoutVault(t *testing.T) {
	p := &Plugin{}
	p.Mount(http.NewServeMux(), &plugin.Context{
		DB:    mcpTestDB(t),
		Guard: func(h http.Handler) http.Handler { return h },
		Host:  &plugin.TestHost{},
	})

	if err := p.Validate(context.Background(), &plugin.TestHost{}); err == nil {
		t.Fatal("a server mount without a vault started anyway")
	}
	if err := p.Validate(context.Background(), nil); err == nil {
		t.Fatal("a nil host started anyway")
	}
}

// A server mount keeps the vault, so the write tools stay available even when
// an MCP connection re-mounts the plugin.
func TestHTTPMountKeepsWriteTools(t *testing.T) {
	p := &Plugin{}
	p.Mount(http.NewServeMux(), &plugin.Context{
		DB:    mcpTestDB(t),
		Guard: func(h http.Handler) http.Handler { return h },
		Host:  &plugin.TestHost{CryptoMock: newFakeVault()},
	})

	if !p.vaultAvailable() {
		t.Fatal("a server mount with a vault reports none")
	}
	if err := p.Validate(context.Background(), &plugin.TestHost{CryptoMock: newFakeVault()}); err != nil {
		t.Fatalf("server mount refused a real vault: %v", err)
	}
}
