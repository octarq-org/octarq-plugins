package dns

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/octarq-org/octarq-plugins/dns/provider"
)

func TestListDomainsExcludesBaseZoneRows(t *testing.T) {
	p, mkCtx := setupFullDNSTestDB(t)
	seedBase(t, p, "octarq.org")

	for _, name := range []string{"octarq.org", "app.octarq.org", "customer.com"} {
		if err := p.db.Create(&Domain{OrgID: 1, Name: name}).Error; err != nil {
			t.Fatalf("seed domain %s: %v", name, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/domains", nil)
	out, err := p.listDomains(context.Background(), &ListDomainsInput{Ctx: mkCtx(req)})
	if err != nil {
		t.Fatalf("listDomains: %v", err)
	}

	got := map[string]bool{}
	for _, d := range out.Body {
		got[d.Name] = true
	}
	if got["octarq.org"] || got["app.octarq.org"] {
		t.Errorf("listDomains exposed reserved platform hosts: %+v", got)
	}
	if !got["customer.com"] {
		t.Errorf("listDomains dropped a customer domain: %+v", got)
	}
}

func TestListDomainsExcludesSharedHostWhenBaseEmpty(t *testing.T) {
	t.Setenv("OCTARQ_SHARED_HOSTS", "app.octarq.org")
	p, mkCtx := setupFullDNSTestDB(t)

	for _, name := range []string{"app.octarq.org", "customer.com"} {
		if err := p.db.Create(&Domain{OrgID: 1, Name: name}).Error; err != nil {
			t.Fatalf("seed domain %s: %v", name, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/domains", nil)
	out, err := p.listDomains(context.Background(), &ListDomainsInput{Ctx: mkCtx(req)})
	if err != nil {
		t.Fatalf("listDomains: %v", err)
	}

	names := map[string]bool{}
	for _, d := range out.Body {
		names[d.Name] = true
	}
	if names["app.octarq.org"] {
		t.Error("listDomains exposed a registered shared host while no base domain is configured")
	}
	if !names["customer.com"] {
		t.Error("listDomains dropped a customer domain")
	}
}

func TestReconcileDropsReservedPlatformDomain(t *testing.T) {
	p, fake, _ := setupSyncTestPlugin(t)
	seedBase(t, p, "octarq.org")

	reserved := Domain{OrgID: 7, Name: "app.octarq.org", ForLink: true, ProviderAccountID: 3}
	if err := p.db.Create(&reserved).Error; err != nil {
		t.Fatalf("seed reserved domain: %v", err)
	}
	if err := fake.Bind(context.Background(), reserved.OrgID, reserved.Name, true, fmt.Sprintf("dns.apex.%d", reserved.ID)); err != nil {
		t.Fatalf("pre-bind reserved host: %v", err)
	}
	kept := Domain{OrgID: 7, Name: "customer.com", ForLink: true}
	if err := p.db.Create(&kept).Error; err != nil {
		t.Fatalf("seed customer domain: %v", err)
	}

	p.reconcileDomainHosts()

	var n int64
	p.db.Model(&Domain{}).Where("name = ?", "app.octarq.org").Count(&n)
	if n != 0 {
		t.Error("reconcile kept the reserved platform domain row")
	}
	if _, ok := fake.OwnerOf("app.octarq.org"); ok {
		t.Error("reconcile bound the reserved platform host")
	}
	if fake.ServesTraffic("app.octarq.org") {
		t.Error("the reserved platform host still serves traffic after reconcile")
	}
	if owner, ok := fake.OwnerOf("customer.com"); !ok || owner != 7 {
		t.Errorf("customer.com owner = %d, ok = %v; want org 7", owner, ok)
	}
}

func TestSyncDomainsSkipsBaseZone(t *testing.T) {
	p, mkCtx := setupFreshTestDB(t)
	seedBase(t, p, "octarq.org")

	fake := &fakeDNSProvider{zones: []provider.Zone{
		{ID: "z-base", Name: "octarq.org"},
		{ID: "z-app", Name: "app.octarq.org"},
		{ID: "z-customer", Name: "customer.com"},
	}}
	provName := registerFakeProvider(t, "sync-reserved-base", fake)
	acc, err := seedVaultProviderAccount(p, 1, "sync-reserved-base-acc", provName, map[string]any{"apiToken": "tok"})
	if err != nil {
		t.Fatalf("seed provider account: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/domains/sync", nil)
	out, err := p.syncDomains(context.Background(), &SyncDomainsInput{
		Ctx: mkCtx(req),
		Body: struct {
			ProviderAccountID uint `json:"providerAccountId,omitempty"`
		}{acc.ID},
	})
	if err != nil {
		t.Fatalf("syncDomains: %v", err)
	}
	if out.Body["created"] != 1 {
		t.Errorf("created = %v, want 1 (only the customer zone)", out.Body["created"])
	}
	var names []string
	p.db.Model(&Domain{}).Pluck("name", &names)
	if len(names) != 1 || names[0] != "customer.com" {
		t.Errorf("synced domains = %v, want [customer.com]", names)
	}
}

func TestSyncDomainsSkipsSharedHostWhenBaseEmpty(t *testing.T) {
	t.Setenv("OCTARQ_SHARED_HOSTS", "app.octarq.org")
	p, mkCtx := setupFreshTestDB(t)

	fake := &fakeDNSProvider{zones: []provider.Zone{
		{ID: "z-app", Name: "app.octarq.org"},
		{ID: "z-customer", Name: "customer.com"},
	}}
	provName := registerFakeProvider(t, "sync-reserved-shared", fake)
	acc, err := seedVaultProviderAccount(p, 1, "sync-reserved-shared-acc", provName, map[string]any{"apiToken": "tok"})
	if err != nil {
		t.Fatalf("seed provider account: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/domains/sync", nil)
	out, err := p.syncDomains(context.Background(), &SyncDomainsInput{
		Ctx: mkCtx(req),
		Body: struct {
			ProviderAccountID uint `json:"providerAccountId,omitempty"`
		}{acc.ID},
	})
	if err != nil {
		t.Fatalf("syncDomains: %v", err)
	}
	if out.Body["created"] != 1 {
		t.Errorf("created = %v, want 1 (shared platform host must not be imported)", out.Body["created"])
	}
	var n int64
	p.db.Model(&Domain{}).Where("name = ?", "app.octarq.org").Count(&n)
	if n != 0 {
		t.Error("sync imported the registered shared host while no base domain is configured")
	}
}

func TestReconcileRetainsProvisionedSubdomain(t *testing.T) {
	p, fake, _ := setupSyncTestPlugin(t)
	seedBase(t, p, "octarq.org")

	provisioned := Domain{OrgID: 9, Name: "tenant9.octarq.org", ForLink: true}
	if err := p.db.Create(&provisioned).Error; err != nil {
		t.Fatalf("seed provisioned subdomain: %v", err)
	}
	if err := fake.Bind(context.Background(), provisioned.OrgID, provisioned.Name, true, fmt.Sprintf("dns.apex.%d", provisioned.ID)); err != nil {
		t.Fatalf("pre-bind provisioned host: %v", err)
	}

	p.reconcileDomainHosts()

	var n int64
	p.db.Model(&Domain{}).Where("name = ?", "tenant9.octarq.org").Count(&n)
	if n != 1 {
		t.Fatal("reconcile deleted a provisioned tenant subdomain row")
	}
	if _, ok := fake.OwnerOf("tenant9.octarq.org"); ok {
		t.Error("reconcile left the reserved tenant subdomain bound")
	}
}

func TestCreateDomainRejectsSharedHostWhenBaseEmpty(t *testing.T) {
	t.Setenv("OCTARQ_SHARED_HOSTS", "app.octarq.org")
	p, mkCtx := setupFullDNSTestDB(t)
	acc := ProviderAccount{OrgID: 1, Name: "P", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("seed provider account: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	if _, err := p.createDomain(context.Background(), &CreateDomainInput{
		Ctx:  mkCtx(req),
		Body: domainDTO{Name: "app.octarq.org", ProviderAccountID: acc.ID},
	}); err == nil {
		t.Error("createDomain accepted a registered shared host while no base domain is configured")
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	if _, err := p.createDomain(context.Background(), &CreateDomainInput{
		Ctx:  mkCtx(req2),
		Body: domainDTO{Name: "customer.com", ProviderAccountID: acc.ID},
	}); err != nil {
		t.Errorf("createDomain(customer.com) = %v, want success", err)
	}
}
