package dns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateDomainRejectsHostListsOutsideZone(t *testing.T) {
	p, mkCtx := setupFullDNSTestDB(t)
	acc := ProviderAccount{OrgID: 1, Name: "P", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("seed provider account: %v", err)
	}

	ctx := context.Background()
	enabled := true
	rejected := []domainDTO{
		{Name: "example.com", ProviderAccountID: acc.ID, LinkHosts: &[]hostEntry{{Host: "victim.test", Enabled: &enabled}}},
		{Name: "example.com", ProviderAccountID: acc.ID, MailHosts: &[]hostEntry{{Host: "evil.net", Enabled: &enabled}}},
		{Name: "example.com", ProviderAccountID: acc.ID, LinkHosts: &[]hostEntry{{Host: "notexample.com", Enabled: &enabled}}},
		{Name: "example.com", ProviderAccountID: acc.ID, MailHosts: &[]hostEntry{{Host: "HTTPS://Victim.Test:8443", Enabled: &enabled}}},
	}
	for _, dto := range rejected {
		req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
		if _, err := p.createDomain(ctx, &CreateDomainInput{Ctx: mkCtx(req), Body: dto}); err == nil {
			t.Errorf("createDomain accepted an outside-zone host list entry: %+v", dto)
		}
		var n int64
		p.db.Model(&Domain{}).Where("name = ?", "example.com").Count(&n)
		if n != 0 {
			t.Fatalf("a rejected createDomain wrote a row")
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/dns/domains", nil)
	if _, err := p.createDomain(ctx, &CreateDomainInput{
		Ctx: mkCtx(req),
		Body: domainDTO{Name: "example.com", ProviderAccountID: acc.ID,
			LinkHosts: &[]hostEntry{{Host: "example.com", Enabled: &enabled}, {Host: "go.example.com", Enabled: &enabled}},
			MailHosts: &[]hostEntry{{Host: "mail.example.com", Enabled: &enabled}}},
	}); err != nil {
		t.Errorf("createDomain with apex/subdomain host lists = %v, want success", err)
	}
}

func TestUpdateDomainRejectsHostListsOutsideZone(t *testing.T) {
	p, mkCtx := setupFullDNSTestDB(t)
	acc := ProviderAccount{OrgID: 1, Name: "P", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatalf("seed provider account: %v", err)
	}
	dom := Domain{OrgID: 1, Name: "example.com", ProviderAccountID: acc.ID}
	if err := p.db.Create(&dom).Error; err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	ctx := context.Background()
	enabled := true
	req := httptest.NewRequest(http.MethodPut, "/api/dns/domains/1", nil)
	if _, err := p.updateDomain(ctx, &UpdateDomainInput{
		Ctx:  mkCtx(req),
		ID:   dom.ID,
		Body: domainDTO{LinkHosts: &[]hostEntry{{Host: "victim.test", Enabled: &enabled}}},
	}); err == nil {
		t.Error("updateDomain accepted an outside-zone link host")
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
		Body: domainDTO{LinkHosts: &[]hostEntry{{Host: "go.example.com", Enabled: &enabled}}, MailHosts: &[]hostEntry{{Host: "mail.example.com", Enabled: &enabled}}},
	}); err != nil {
		t.Errorf("updateDomain with in-zone host lists = %v, want success", err)
	}
	if err := p.db.First(&after, dom.ID).Error; err != nil {
		t.Fatalf("reload domain: %v", err)
	}
	if len(after.LinkHosts) != 1 || after.LinkHosts[0].Host != "go.example.com" {
		t.Errorf("accepted update did not persist the host list: %+v", after.LinkHosts)
	}
}
