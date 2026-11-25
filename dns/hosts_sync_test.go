package dns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/octarq-org/octarq-plugins/dns/provider"
	"github.com/octarq-org/octarq/server/plugin"
)

func setupSyncTestPlugin(t *testing.T) (*Plugin, *fakeHostRegistry, func(req *http.Request) huma.Context) {
	t.Helper()
	p, mkCtx := setupFullDNSTestDB(t)
	fake := newFakeHostRegistry()
	p.SetHost(&plugin.TestHost{
		HostsMock: fake,
	})
	return p, fake, mkCtx
}

func TestHostSync_CreateDomainBindsHost(t *testing.T) {
	p, fake, mkCtx := setupSyncTestPlugin(t)
	ctx := context.Background()

	acc := ProviderAccount{OrgID: 1, Name: "P1", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	forLink := true
	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	out, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx: mkCtx(req),
		Body: domainDTO{
			Name:              "acme.org",
			ProviderAccountID: acc.ID,
			ForLink:           &forLink,
		},
	})
	if err != nil {
		t.Fatalf("createDomain: %v", err)
	}

	owner, ok := fake.OwnerOf("acme.org")
	if !ok || owner != 1 {
		t.Fatalf("expected acme.org to be owned by org 1, got owner=%d ok=%v", owner, ok)
	}
	if !fake.ServesTraffic("acme.org") {
		t.Fatal("expected acme.org to serve traffic")
	}
	if out.Body.ID == 0 {
		t.Fatal("expected created domain to have non-zero ID")
	}
}

func TestHostSync_UpdateHostListSwapsBindings(t *testing.T) {
	p, fake, mkCtx := setupSyncTestPlugin(t)
	ctx := context.Background()

	acc := ProviderAccount{OrgID: 1, Name: "P1", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	forLink := true
	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	out, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx: mkCtx(req),
		Body: domainDTO{
			Name:              "acme.org",
			ProviderAccountID: acc.ID,
			ForLink:           &forLink,
		},
	})
	if err != nil {
		t.Fatalf("createDomain: %v", err)
	}

	enabled := true
	reqUpdate := httptest.NewRequest(http.MethodPut, "/api/dns/domains/1", nil)
	_, err = p.updateDomain(ctx, &UpdateDomainInput{
		Ctx: mkCtx(reqUpdate),
		ID:  out.Body.ID,
		Body: domainDTO{
			LinkHosts: &[]hostEntry{
				{Host: "go.acme.org", Enabled: &enabled},
			},
		},
	})
	if err != nil {
		t.Fatalf("updateDomain: %v", err)
	}

	ownerGo, okGo := fake.OwnerOf("go.acme.org")
	if !okGo || ownerGo != 1 {
		t.Fatalf("expected go.acme.org to be owned by org 1, got owner=%d ok=%v", ownerGo, okGo)
	}

	if ownerApex, okApex := fake.OwnerOf("acme.org"); !okApex || ownerApex != 1 {
		t.Fatalf("apex acme.org owner = %d, ok = %v; want the workspace that owns the domain", ownerApex, okApex)
	}
	if fake.ServesTraffic("acme.org") {
		t.Fatal("apex acme.org must not serve traffic while specific link hosts exist")
	}
}

func TestHostSync_DisableForLinkUnbinds(t *testing.T) {
	p, fake, mkCtx := setupSyncTestPlugin(t)
	ctx := context.Background()

	acc := ProviderAccount{OrgID: 1, Name: "P1", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	forLink := true
	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	out, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx: mkCtx(req),
		Body: domainDTO{
			Name:              "acme.org",
			ProviderAccountID: acc.ID,
			ForLink:           &forLink,
		},
	})
	if err != nil {
		t.Fatalf("createDomain: %v", err)
	}

	disabled := false
	reqUpdate := httptest.NewRequest(http.MethodPut, "/api/dns/domains/1", nil)
	_, err = p.updateDomain(ctx, &UpdateDomainInput{
		Ctx: mkCtx(reqUpdate),
		ID:  out.Body.ID,
		Body: domainDTO{
			ForLink: &disabled,
			ForMail: &disabled,
		},
	})
	if err != nil {
		t.Fatalf("updateDomain: %v", err)
	}

	if owner, ok := fake.OwnerOf("acme.org"); !ok || owner != 1 {
		t.Fatalf("acme.org owner = %d, ok = %v; want the workspace to keep the domain", owner, ok)
	}
	if fake.ServesTraffic("acme.org") {
		t.Fatal("expected ServesTraffic to be false after disabling")
	}
}

func TestHostSync_DeleteAndPurgeUnbind(t *testing.T) {
	p, fake, mkCtx := setupSyncTestPlugin(t)
	ctx := context.Background()

	acc := ProviderAccount{OrgID: 1, Name: "P1", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	forLink := true
	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	out, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx: mkCtx(req),
		Body: domainDTO{
			Name:              "delete-me.org",
			ProviderAccountID: acc.ID,
			ForLink:           &forLink,
		},
	})
	if err != nil {
		t.Fatalf("createDomain: %v", err)
	}

	if _, ok := fake.OwnerOf("delete-me.org"); !ok {
		t.Fatal("expected delete-me.org to be bound")
	}

	reqDel := httptest.NewRequest(http.MethodDelete, "/api/dns/domains/1", nil)
	if _, err := p.deleteDomain(ctx, &DeleteDomainInput{Ctx: mkCtx(reqDel), ID: out.Body.ID}); err != nil {
		t.Fatalf("deleteDomain: %v", err)
	}

	if _, ok := fake.OwnerOf("delete-me.org"); ok {
		t.Fatal("expected delete-me.org to be unbound after deletion")
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	_, err = p.createDomain(ctx, &CreateDomainInput{
		Ctx: mkCtx(req2),
		Body: domainDTO{
			Name:              "purge-me.org",
			ProviderAccountID: acc.ID,
			ForLink:           &forLink,
		},
	})
	if err != nil {
		t.Fatalf("createDomain: %v", err)
	}

	if _, ok := fake.OwnerOf("purge-me.org"); !ok {
		t.Fatal("expected purge-me.org to be bound")
	}

	if err := p.purge(1); err != nil {
		t.Fatalf("purge: %v", err)
	}

	if _, ok := fake.OwnerOf("purge-me.org"); ok {
		t.Fatal("expected purge-me.org to be unbound after purge")
	}
}

func TestHostSync_ReplaceOwnedConflictFails(t *testing.T) {
	p, fake, mkCtx := setupSyncTestPlugin(t)
	ctx := context.Background()

	acc1 := ProviderAccount{OrgID: 1, Name: "P1", Type: "cloudflare"}
	if err := p.db.Create(&acc1).Error; err != nil {
		t.Fatalf("create provider account 1: %v", err)
	}
	acc2 := ProviderAccount{OrgID: 2, Name: "P2", Type: "cloudflare"}
	if err := p.db.Create(&acc2).Error; err != nil {
		t.Fatalf("create provider account 2: %v", err)
	}

	forLink := true
	req1 := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	req1.Header.Set("X-Org-ID", "1")
	_, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx: mkCtx(req1),
		Body: domainDTO{
			Name:              "shared-conflict.org",
			ProviderAccountID: acc1.ID,
			ForLink:           &forLink,
		},
	})
	if err != nil {
		t.Fatalf("createDomain org 1: %v", err)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	req2.Header.Set("X-Org-ID", "2")
	_, err = p.createDomain(ctx, &CreateDomainInput{
		Ctx: mkCtx(req2),
		Body: domainDTO{
			Name:              "shared-conflict.org",
			ProviderAccountID: acc2.ID,
			ForLink:           &forLink,
		},
	})
	if err == nil {
		t.Fatal("expected conflict error when org 2 tries to bind already bound host")
	}

	var count int64
	p.db.Model(&Domain{}).Where("owner_id = ? AND name = ?", 2, "shared-conflict.org").Count(&count)
	if count != 0 {
		t.Fatalf("expected no row created for org 2, found %d", count)
	}

	owner, ok := fake.OwnerOf("shared-conflict.org")
	if !ok || owner != 1 {
		t.Fatalf("expected org 1 to remain owner of shared-conflict.org, got owner=%d ok=%v", owner, ok)
	}
}

func TestHostSync_ForMailOnlyApexServes(t *testing.T) {
	p, fake, mkCtx := setupSyncTestPlugin(t)
	ctx := context.Background()

	acc := ProviderAccount{OrgID: 1, Name: "P1", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	forLink := false
	forMail := true
	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	out, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx: mkCtx(req),
		Body: domainDTO{
			Name:              "mailonly.org",
			ProviderAccountID: acc.ID,
			ForLink:           &forLink,
			ForMail:           &forMail,
		},
	})
	if err != nil {
		t.Fatalf("createDomain: %v", err)
	}

	owner, ok := fake.OwnerOf("mailonly.org")
	if !ok || owner != 1 {
		t.Fatalf("expected mailonly.org to be owned by org 1, got owner=%d ok=%v", owner, ok)
	}
	if !fake.ServesTraffic("mailonly.org") {
		t.Fatal("expected the mail-only apex to serve traffic because no explicit mail hosts exist")
	}
	if out.Body.ID == 0 {
		t.Fatal("expected created domain to have non-zero ID")
	}
}

func TestHostSync_LinkAndMailHostsUnion(t *testing.T) {
	p, fake, mkCtx := setupSyncTestPlugin(t)
	ctx := context.Background()

	acc := ProviderAccount{OrgID: 1, Name: "P1", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("create provider account: %v", err)
	}

	forLink := true
	forMail := true
	enabled := true
	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	out, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx: mkCtx(req),
		Body: domainDTO{
			Name:              "union.org",
			ProviderAccountID: acc.ID,
			ForLink:           &forLink,
			ForMail:           &forMail,
			LinkHosts: &[]hostEntry{
				{Host: "link.union.org", Enabled: &enabled},
			},
			MailHosts: &[]hostEntry{
				{Host: "mail.union.org", Enabled: &enabled},
			},
		},
	})
	if err != nil {
		t.Fatalf("createDomain: %v", err)
	}

	if owner, ok := fake.OwnerOf("link.union.org"); !ok || owner != 1 {
		t.Fatalf("expected link.union.org to be owned by org 1, got owner=%d ok=%v", owner, ok)
	}
	if owner, ok := fake.OwnerOf("mail.union.org"); !ok || owner != 1 {
		t.Fatalf("expected mail.union.org to be owned by org 1, got owner=%d ok=%v", owner, ok)
	}
	if !fake.ServesTraffic("link.union.org") {
		t.Fatal("expected link.union.org to serve traffic")
	}
	if out.Body.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
}

func TestHostSync_MCPUpdateDomainConfig(t *testing.T) {
	p, fake, _ := setupSyncTestPlugin(t)
	ctx := plugin.WithOrgID(context.Background(), 1)

	dom := Domain{
		OrgID:   1,
		Name:    "mcp-test.org",
		ForLink: false,
		ForMail: false,
	}
	if err := p.db.Create(&dom).Error; err != nil {
		t.Fatalf("create domain: %v", err)
	}

	forLink := true
	_, _, err := p.mcpUpdateDomainConfig(ctx, nil, updateDomainConfigInput{
		ID:      dom.ID,
		ForLink: &forLink,
	})
	if err != nil {
		t.Fatalf("mcpUpdateDomainConfig: %v", err)
	}

	owner, ok := fake.OwnerOf("mcp-test.org")
	if !ok || owner != 1 {
		t.Fatalf("expected mcp-test.org to be owned by org 1, got owner=%d ok=%v", owner, ok)
	}
	if !fake.ServesTraffic("mcp-test.org") {
		t.Fatal("expected mcp-test.org to serve traffic after MCP update")
	}
}

func TestSyncDomainsReportsHostBindConflict(t *testing.T) {
	p, mkCtx := setupFreshTestDB(t)
	fake := newFakeHostRegistry()
	p.SetHost(&plugin.TestHost{HostsMock: fake})

	ctx := context.Background()
	if err := fake.Bind(ctx, 2, "taken.example.com", true, "other-plugin"); err != nil {
		t.Fatalf("pre-bind conflicting host: %v", err)
	}

	fakeProv := &fakeDNSProvider{zones: []provider.Zone{{ID: "z-taken", Name: "taken.example.com"}}}
	provName := registerFakeProvider(t, "sync-bind-conflict", fakeProv)
	acc, err := seedVaultProviderAccount(p, 1, "sync-bind-conflict-acc", provName, map[string]any{"apiToken": "tok"})
	if err != nil {
		t.Fatalf("seed provider account: %v", err)
	}
	if err := p.db.Create(&Domain{OrgID: 1, Name: "taken.example.com", ProviderAccountID: acc.ID, ZoneID: "z-old", ForLink: true}).Error; err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/domains/sync", nil)
	_, err = p.syncDomains(ctx, &SyncDomainsInput{
		Ctx: mkCtx(req),
		Body: struct {
			ProviderAccountID uint `json:"providerAccountId,omitempty"`
		}{acc.ID},
	})
	if err == nil {
		t.Fatal("expected conflict error when host binding fails")
	}
	if statusOf(t, err) != http.StatusConflict {
		t.Fatalf("status = %d, want %d", statusOf(t, err), http.StatusConflict)
	}
	if !strings.Contains(err.Error(), "taken.example.com") {
		t.Fatalf("error %q does not name the domain that could not be registered", err)
	}
}

func TestSyncDomainsSucceedsWhenHostsBind(t *testing.T) {
	p, mkCtx := setupFreshTestDB(t)
	fake := newFakeHostRegistry()
	p.SetHost(&plugin.TestHost{HostsMock: fake})

	ctx := context.Background()
	fakeProv := &fakeDNSProvider{zones: []provider.Zone{{ID: "z-free", Name: "free.example.com"}}}
	provName := registerFakeProvider(t, "sync-bind-ok", fakeProv)
	acc, err := seedVaultProviderAccount(p, 1, "sync-bind-ok-acc", provName, map[string]any{"apiToken": "tok"})
	if err != nil {
		t.Fatalf("seed provider account: %v", err)
	}
	if err := p.db.Create(&Domain{OrgID: 1, Name: "free.example.com", ProviderAccountID: acc.ID, ZoneID: "z-old", ForLink: true}).Error; err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/domains/sync", nil)
	out, err := p.syncDomains(ctx, &SyncDomainsInput{
		Ctx: mkCtx(req),
		Body: struct {
			ProviderAccountID uint `json:"providerAccountId,omitempty"`
		}{acc.ID},
	})
	if err != nil {
		t.Fatalf("syncDomains: %v", err)
	}
	if out.Body["updated"] != 1 {
		t.Fatalf("updated = %v, want 1", out.Body["updated"])
	}
	owner, ok := fake.OwnerOf("free.example.com")
	if !ok || owner != 1 {
		t.Fatalf("expected free.example.com to be owned by org 1, got owner=%d ok=%v", owner, ok)
	}
}
