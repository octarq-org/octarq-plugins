package dns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/octarq-org/octarq/server/origin"
	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"github.com/octarq-org/octarq/server/plugin"
)

func seedBase(t *testing.T, p *Plugin, base string) {
	t.Helper()
	if err := p.db.Table("settings").Create(&testSetting{Key: "base_domain", Value: base}).Error; err != nil {
		t.Fatalf("seed base domain: %v", err)
	}
}

func TestCreateDomainRejectsReservedBaseZone(t *testing.T) {
	p, mkCtx := setupFullDNSTestDB(t)
	seedBase(t, p, "app.octarq.org")
	acc := ProviderAccount{OrgID: 1, Name: "P", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("seed provider account: %v", err)
	}
	if err := p.db.Table("org_slug_histories").Create(&testOrgSlugHistory{Slug: "oldslug", OrgID: 1, RetiredAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed slug history: %v", err)
	}

	ctx := context.Background()
	rejected := []string{
		"app.octarq.org",
		"nobody.app.octarq.org",
		"oldslug.app.octarq.org",
	}
	for _, name := range rejected {
		req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
		_, err := p.createDomain(ctx, &CreateDomainInput{
			Ctx:  mkCtx(req),
			Body: domainDTO{Name: name, ProviderAccountID: acc.ID},
		})
		if err == nil {
			t.Errorf("createDomain(%q) succeeded — the reserved base zone must not be manually registrable", name)
		}
		var n int64
		p.db.Model(&Domain{}).Where("name = ?", name).Count(&n)
		if n != 0 {
			t.Errorf("createDomain(%q) wrote a row despite rejection", name)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	if _, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx:  mkCtx(req),
		Body: domainDTO{Name: "customer.com", ProviderAccountID: acc.ID},
	}); err != nil {
		t.Errorf("createDomain(customer.com) = %v, want success", err)
	}
}

func TestCreateDomainAllowsBaseZoneWhenUnconfigured(t *testing.T) {
	p, mkCtx := setupFullDNSTestDB(t)
	acc := ProviderAccount{OrgID: 1, Name: "P", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("seed provider account: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	if _, err := p.createDomain(context.Background(), &CreateDomainInput{
		Ctx:  mkCtx(req),
		Body: domainDTO{Name: "app.octarq.org", ProviderAccountID: acc.ID},
	}); err != nil {
		t.Errorf("createDomain with no base configured = %v, want success (today's behaviour)", err)
	}
}

func TestUnderBaseZonePredicate(t *testing.T) {
	p, _ := setupFullDNSTestDB(t)
	seedBase(t, p, "app.octarq.org")

	if !underBaseZone(p.db, "app.octarq.org") {
		t.Error("the base itself must be reserved")
	}
	if !underBaseZone(p.db, "acme7x.app.octarq.org") {
		t.Error("a subdomain of the base must be reserved")
	}
	if !underBaseZone(p.db, "deep.sub.app.octarq.org") {
		t.Error("any depth under the base must be reserved")
	}
	if !underBaseZone(p.db, "HTTPS://Acme7x.App.Octarq.Org:8443") {
		t.Error("a normalized subdomain of the base must be reserved")
	}
	if underBaseZone(p.db, "octarq.org") {
		t.Error("the apex above the base is not itself reserved")
	}
	if underBaseZone(p.db, "app.octarq.org.evil.com") {
		t.Error("a suffix lookalike of the base must not be reserved")
	}
	if underBaseZone(p.db, "customer.com") {
		t.Error("an unrelated domain must not be reserved")
	}

	t.Run("unconfigured", func(t *testing.T) {
		p2, _ := setupFullDNSTestDB(t)
		if underBaseZone(p2.db, "app.octarq.org") {
			t.Error("without a configured base no zone is reserved")
		}
	})
}

func TestCreateDomainRejectsReservedHostLists(t *testing.T) {
	p, mkCtx := setupFullDNSTestDB(t)
	seedBase(t, p, "app.octarq.org")
	acc := ProviderAccount{OrgID: 1, Name: "P", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("seed provider account: %v", err)
	}

	ctx := context.Background()
	enabled := true
	rejected := []domainDTO{
		{Name: "evil.com", ProviderAccountID: acc.ID, LinkHosts: &[]hostEntry{{Host: "victim7x.app.octarq.org", Enabled: &enabled}}},
		{Name: "evil.com", ProviderAccountID: acc.ID, MailHosts: &[]hostEntry{{Host: "victim8y.app.octarq.org", Enabled: &enabled}}},
		{Name: "evil.com", ProviderAccountID: acc.ID, LinkHosts: &[]hostEntry{{Host: "app.octarq.org", Enabled: &enabled}}},
		{Name: "evil.com", ProviderAccountID: acc.ID, MailHosts: &[]hostEntry{{Host: "HTTPS://Victim9x.App.Octarq.Org:8443", Enabled: &enabled}}},
	}
	for _, dto := range rejected {
		req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
		if _, err := p.createDomain(ctx, &CreateDomainInput{Ctx: mkCtx(req), Body: dto}); err == nil {
			t.Errorf("createDomain with a reserved host-list entry succeeded: %+v", dto)
		}
		var n int64
		p.db.Model(&Domain{}).Where("name = ?", "evil.com").Count(&n)
		if n != 0 {
			t.Fatalf("a rejected createDomain wrote a row")
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	if _, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx:  mkCtx(req),
		Body: domainDTO{Name: "evil.com", ProviderAccountID: acc.ID, LinkHosts: &[]hostEntry{{Host: "go.evil.com", Enabled: &enabled}}},
	}); err != nil {
		t.Errorf("createDomain with an unrelated link host = %v, want success", err)
	}
}

func TestUpdateDomainRejectsReservedHostLists(t *testing.T) {
	p, mkCtx := setupFullDNSTestDB(t)
	seedBase(t, p, "app.octarq.org")
	acc := ProviderAccount{OrgID: 1, Name: "P", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("seed provider account: %v", err)
	}
	dom := Domain{OrgID: 1, Name: "evil.com", ProviderAccountID: acc.ID}
	if err := p.db.Create(&dom).Error; err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	ctx := context.Background()
	enabled := true
	req := httptest.NewRequest(http.MethodPut, "/api/dns/domains/1", nil)
	if _, err := p.updateDomain(ctx, &UpdateDomainInput{
		Ctx:  mkCtx(req),
		ID:   dom.ID,
		Body: domainDTO{LinkHosts: &[]hostEntry{{Host: "victim7x.app.octarq.org", Enabled: &enabled}}},
	}); err == nil {
		t.Error("updateDomain with a reserved link host succeeded")
	}
	var after Domain
	if err := p.db.First(&after, dom.ID).Error; err != nil {
		t.Fatalf("reload domain: %v", err)
	}
	if len(after.LinkHosts) != 0 {
		t.Errorf("a rejected update still wrote the host list: %+v", after.LinkHosts)
	}

	req2 := httptest.NewRequest(http.MethodPut, "/api/dns/domains/1", nil)
	if _, err := p.updateDomain(ctx, &UpdateDomainInput{
		Ctx:  mkCtx(req2),
		ID:   dom.ID,
		Body: domainDTO{LinkHosts: &[]hostEntry{{Host: "go.evil.com", Enabled: &enabled}}},
	}); err != nil {
		t.Errorf("updateDomain with an unrelated link host = %v, want success", err)
	}
}

func TestPurgeRemovesProvisionedSubdomain(t *testing.T) {
	p, _ := setupFullDNSTestDB(t)
	rv := newFakeHostRegistry()
	p.SetHost(&plugin.TestHost{HostsMock: rv})
	seedBase(t, p, "app.example.com")
	dom1 := Domain{OrgID: 42, Name: "doomed.app.example.com", ForLink: true, LinkHosts: hostlist.HostList{{Host: "doomed.app.example.com", Enabled: true}}}
	dom2 := Domain{OrgID: 43, Name: "bystander.app.example.com", ForLink: true, LinkHosts: hostlist.HostList{{Host: "bystander.app.example.com", Enabled: true}}}
	p.db.Create(&dom1)
	p.db.Create(&dom2)
	_ = p.syncDomainHosts(context.Background(), &dom1)
	_ = p.syncDomainHosts(context.Background(), &dom2)
	origin.ClearHostCache(dom1.Name)
	origin.ClearHostCache(dom2.Name)
	if owner, ok := rv.OwnerOf("doomed.app.example.com"); !ok || owner != 42 {
		t.Fatalf("OwnerOf before purge = (%d, %v), want (42, true)", owner, ok)
	}

	if err := p.purge(42); err != nil {
		t.Fatalf("purge: %v", err)
	}

	var n int64
	p.db.Model(&Domain{}).Where("name = ?", "doomed.app.example.com").Count(&n)
	if n != 0 {
		t.Errorf("purged workspace's subdomain row still present")
	}
	if owner, ok := rv.OwnerOf("doomed.app.example.com"); ok || owner != 0 {
		t.Errorf("OwnerOf after purge = (%d, %v), want (0, false)", owner, ok)
	}
	if owner, ok := rv.OwnerOf("bystander.app.example.com"); !ok || owner != 43 {
		t.Errorf("bystander subdomain after purge = (%d, %v), want (43, true)", owner, ok)
	}
}
