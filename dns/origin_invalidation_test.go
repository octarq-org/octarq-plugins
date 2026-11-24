package dns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/octarq-org/octarq/server/origin"
	"github.com/octarq-org/octarq/server/plugin"
)

func TestDomainWritesInvalidateOriginCache(t *testing.T) {
	p, mkCtx := setupFullDNSTestDB(t)
	rv := newFakeHostRegistry()
	p.SetHost(&plugin.TestHost{HostsMock: rv})
	ctx := context.Background()

	if rv.Any() {
		t.Fatal("a database with no domain rows reported a registered whitelist")
	}
	if _, ok := rv.OwnerOf("app.mydomain.com"); ok {
		t.Fatal("OwnerOf found an owner before any domain was created")
	}

	acc := ProviderAccount{OrgID: 1, Name: "Test Provider", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("seed provider account: %v", err)
	}

	forLink := true
	create := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	if _, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx:  mkCtx(create),
		Body: domainDTO{Name: "mydomain.com", ProviderAccountID: acc.ID, ForLink: &forLink},
	}); err != nil {
		t.Fatalf("createDomain: %v", err)
	}

	if !rv.Any() {
		t.Error("after creating a domain origin still reports no whitelist: the request Host is still being honoured verbatim")
	}
	org, ok := rv.OwnerOf("app.mydomain.com")
	if !ok || org != 1 {
		t.Errorf("OwnerOf(app.mydomain.com) = (%d, %v) after create, want (1, true)", org, ok)
	}

	var created Domain
	if err := p.db.Where("name = ?", "mydomain.com").First(&created).Error; err != nil {
		t.Fatalf("read back created domain: %v", err)
	}

	del := httptest.NewRequest(http.MethodDelete, "/api/dns/domains", nil)
	if _, err := p.deleteDomain(ctx, &DeleteDomainInput{Ctx: mkCtx(del), ID: created.ID}); err != nil {
		t.Fatalf("deleteDomain: %v", err)
	}

	if _, ok := rv.OwnerOf("app.mydomain.com"); ok {
		t.Error("a deleted domain still resolves to its former owner")
	}
}

func TestPurgeInvalidatesOriginCache(t *testing.T) {
	p, _ := setupFullDNSTestDB(t)
	rv := newFakeHostRegistry()
	p.SetHost(&plugin.TestHost{HostsMock: rv})

	dom := Domain{OrgID: 1, Name: "purgeme.example", ForLink: true}
	if err := p.db.Create(&dom).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := p.syncDomainHosts(context.Background(), &dom); err != nil {
		t.Fatalf("sync domain hosts: %v", err)
	}
	origin.ClearHostCache("purgeme.example")
	if org, ok := rv.OwnerOf("app.purgeme.example"); !ok || org != 1 {
		t.Fatalf("OwnerOf = (%d, %v) before purge, want (1, true); the assertion below would be vacuous", org, ok)
	}

	if err := p.purge(1); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, ok := rv.OwnerOf("app.purgeme.example"); ok {
		t.Error("a purged workspace's hostname still resolves to it")
	}
}
