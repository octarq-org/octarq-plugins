package pt

import (
	"context"
	"errors"
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
	if err := db.AutoMigrate(&PTTracker{}, &PTSnapshot{}, &PTSigninResult{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mountHTTP(t *testing.T, p *Plugin, lookup func(string) (any, bool)) {
	t.Helper()
	p.Mount(http.NewServeMux(), &plugin.Context{
		DB:     mcpTestDB(t),
		Guard:  func(h http.Handler) http.Handler { return h },
		Lookup: lookup,
		Host:   &plugin.TestHost{},
	})
}

func TestValidateKeepsTheVaultAcrossTheMCPRemount(t *testing.T) {
	p := &Plugin{}
	reg := plugin.NewRegistry()
	reg.Provide(plugin.ServiceVault, plugin.VaultService(newMockVaultService()))
	mountHTTP(t, p, reg.Lookup)

	p.Mount(nil, &plugin.Context{DB: mcpTestDB(t)})

	if err := p.Validate(context.Background(), plugin.EnsureHost(nil)); err != nil {
		t.Fatalf("MCP re-mount refused: %v", err)
	}
	if !p.vaultAvailable() {
		t.Fatal("the stub mount replaced the real vault context")
	}
}

func TestValidateRefusesHTTPMountWithoutVault(t *testing.T) {
	p := &Plugin{}
	mountHTTP(t, p, nil)

	if err := p.Validate(context.Background(), &plugin.TestHost{}); err == nil {
		t.Fatal("a server mount without a vault started anyway")
	}
	if err := p.Validate(context.Background(), nil); err == nil {
		t.Fatal("a nil host started anyway")
	}
}

func TestValidateRefusesWithoutAnyHost(t *testing.T) {
	p := &Plugin{}
	p.Mount(nil, &plugin.Context{DB: mcpTestDB(t)})

	if err := p.Validate(context.Background(), plugin.EnsureHost(nil)); err == nil {
		t.Fatal("a mount without a vault started anyway")
	}
	if p.vaultAvailable() {
		t.Fatal("a mount without a vault reports available")
	}
}

func TestMCPToolsUseThePersonalOrg(t *testing.T) {
	db := mcpTestDB(t)
	p := &Plugin{db: db, vaultSvc: newMockVaultService()}
	p.ctx = &plugin.Context{
		DB: db,
		PersonalOrgID: func(uid uint) uint {
			if uid == 7 {
				return 101
			}
			return 0
		},
	}

	for _, row := range []PTTracker{
		{OwnerUserID: 7, OrgID: 101, Name: "personal", SecretID: "a", SchemaKey: "nexusphp"},
		{OwnerUserID: 7, OrgID: 1, Name: "team", SecretID: "b", SchemaKey: "nexusphp"},
		{OwnerUserID: 8, OrgID: 101, Name: "other-owner", SecretID: "c", SchemaKey: "nexusphp"},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	ctx := plugin.WithUserID(plugin.WithOrgID(context.Background(), 1), 7)
	_, out, err := p.mcpListTrackers(ctx, nil, mcpTrackerArgs{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	list, ok := out.(mcpListOut)
	if !ok {
		t.Fatalf("unexpected output %T", out)
	}
	if list.Total != 1 || list.Trackers[0].Name != "personal" {
		t.Fatalf("list = %+v", list)
	}

	ctx = plugin.WithUserID(plugin.WithOrgID(context.Background(), 1), 9)
	if _, _, err := p.mcpListTrackers(ctx, nil, mcpTrackerArgs{}); !errors.Is(err, errNoPersonalSpace) {
		t.Fatalf("no personal org = %v", err)
	}

	if _, _, err := p.mcpListTrackers(context.Background(), nil, mcpTrackerArgs{}); !errors.Is(err, errNoActor) {
		t.Fatalf("no actor = %v", err)
	}
}
