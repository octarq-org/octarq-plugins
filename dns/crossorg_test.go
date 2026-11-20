package dns

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq-plugins/dns/provider"
	"gorm.io/gorm"
)

func setupCrossOrgDNSDB(t *testing.T) (*Plugin, *gorm.DB) {
	t.Helper()
	db := freshMemDB(t)
	p := New()
	p.db = db
	p.vaultSvc = newMockVaultService()
	p.audit = func(r *http.Request, action, targetType string, targetID uint, meta map[string]any) {}
	p.orgID = func(r *http.Request) uint {
		if val := r.Header.Get("X-Org-ID"); val != "" {
			var id uint
			fmt.Sscanf(val, "%d", &id)
			return id
		}
		return 1
	}
	p.requireRole = func(r *http.Request, role string) bool {
		return r.Header.Get("X-Role") != "member"
	}
	return p, db
}

func makeDNSOrgCtx(orgID uint, role string) huma.Context {
	req := httptest.NewRequest(http.MethodGet, "/api/domains", nil)
	req.Header.Set("X-Org-ID", fmt.Sprintf("%d", orgID))
	req.Header.Set("X-Role", role)
	return humago.NewContext(nil, req, httptest.NewRecorder())
}

func TestDNSCrossOrgIsolation(t *testing.T) {
	p, db := setupCrossOrgDNSDB(t)
	ctx := context.Background()

	const org1 uint = 1
	const org2 uint = 2

	fake := &fakeDNSProvider{
		records: []provider.Record{
			{ID: "rec-org2-1", Type: "A", Name: "app.org2.example.com", Content: "2.2.2.2", TTL: 300},
		},
	}
	provName := registerFakeProvider(t, "crossorg-dns-prov", fake)

	acc1, err := seedVaultProviderAccount(p, org1, "org1-cf", provName, map[string]any{"apiToken": "t1"})
	if err != nil {
		t.Fatal(err)
	}
	dom1 := Domain{OrgID: org1, Name: "org1.example.com", ProviderAccountID: acc1.ID, ZoneID: "zone-1", Note: "Org1 Domain"}
	db.Create(&dom1)
	tok1 := DDNSToken{
		OrgID: org1, DomainID: dom1.ID, RecordName: "ddns1", RecordType: "A",
		TokenHash: "hash1", Label: "Org1 DDNS", CreatedAt: time.Now(),
	}
	db.Create(&tok1)

	acc2, err := seedVaultProviderAccount(p, org2, "org2-cf", provName, map[string]any{"apiToken": "t2"})
	if err != nil {
		t.Fatal(err)
	}
	dom2 := Domain{OrgID: org2, Name: "org2.example.com", ProviderAccountID: acc2.ID, ZoneID: "zone-2", Note: "Org2 Domain"}
	db.Create(&dom2)
	tok2 := DDNSToken{
		OrgID: org2, DomainID: dom2.ID, RecordName: "ddns2", RecordType: "A",
		TokenHash: "hash2", Label: "Org2 DDNS", CreatedAt: time.Now(),
	}
	db.Create(&tok2)

	ctxOrg1Admin := makeDNSOrgCtx(org1, "admin")
	ctxOrg2Admin := makeDNSOrgCtx(org2, "admin")

	domListOut, err := p.listDomains(ctx, &ListDomainsInput{Ctx: ctxOrg1Admin})
	if err != nil {
		t.Fatalf("listDomains: %v", err)
	}
	if len(domListOut.Body) != 1 || domListOut.Body[0].ID != dom1.ID {
		t.Errorf("listDomains: Org 1 saw domains %+v, want only dom1 (%d)", domListOut.Body, dom1.ID)
	}

	_, err = p.createDomain(ctx, &CreateDomainInput{
		Ctx: ctxOrg1Admin,
		Body: domainDTO{
			Name:              "rogue.example.com",
			ProviderAccountID: acc2.ID,
		},
	})
	if err == nil {
		t.Errorf("createDomain: expected error when Org 1 binds Org 2 provider account, got nil")
	}
	var count int64
	db.Model(&Domain{}).Where("name = ?", "rogue.example.com").Count(&count)
	if count != 0 {
		t.Errorf("createDomain: rogue domain was created using Org 2 provider account")
	}

	maliciousNote := "malicious-domain-update"
	_, err = p.updateDomain(ctx, &UpdateDomainInput{
		Ctx:  ctxOrg1Admin,
		ID:   dom2.ID,
		Body: domainDTO{Note: maliciousNote},
	})
	if err == nil {
		t.Errorf("updateDomain: expected error when Org 1 updates Org 2 domain, got nil")
	}
	var dom2Reload Domain
	db.First(&dom2Reload, dom2.ID)
	if dom2Reload.Note == maliciousNote {
		t.Fatalf("updateDomain: Org 2 domain was modified by Org 1")
	}

	_, err = p.updateDomain(ctx, &UpdateDomainInput{
		Ctx:  ctxOrg1Admin,
		ID:   dom1.ID,
		Body: domainDTO{ProviderAccountID: acc2.ID},
	})
	if err == nil {
		t.Errorf("updateDomain: expected error when Org 1 rebinds to Org 2 provider account, got nil")
	}
	var dom1Reload Domain
	db.First(&dom1Reload, dom1.ID)
	if dom1Reload.ProviderAccountID == acc2.ID {
		t.Fatalf("updateDomain: Org 1 domain bound to Org 2 provider account")
	}

	_, err = p.deleteDomain(ctx, &DeleteDomainInput{
		Ctx: ctxOrg1Admin,
		ID:  dom2.ID,
	})
	if err == nil {
		t.Errorf("deleteDomain: expected error when Org 1 deletes Org 2 domain, got nil")
	}
	db.Model(&Domain{}).Where("id = ?", dom2.ID).Count(&count)
	if count == 0 {
		t.Fatalf("deleteDomain: Org 2 domain was deleted by Org 1")
	}

	_, err = p.listRecords(ctx, &ListRecordsInput{
		Ctx: ctxOrg1Admin,
		ID:  dom2.ID,
	})
	if err == nil {
		t.Errorf("listRecords: expected error when Org 1 lists records of Org 2 domain, got nil")
	}

	_, err = p.createRecord(ctx, &CreateRecordInput{
		Ctx: ctxOrg1Admin,
		ID:  dom2.ID,
		Body: provider.Record{
			Type:    "A",
			Name:    "injected.org2.example.com",
			Content: "9.9.9.9",
		},
	})
	if err == nil {
		t.Errorf("createRecord: expected error when Org 1 creates record in Org 2 domain, got nil")
	}
	for _, cr := range fake.created {
		if cr.Name == "injected.org2.example.com" {
			t.Fatalf("createRecord: record was injected into Org 2 domain zone")
		}
	}

	_, err = p.updateRecord(ctx, &UpdateRecordInput{
		Ctx: ctxOrg1Admin,
		ID:  dom2.ID,
		RID: "rec-org2-1",
		Body: provider.Record{
			Type:    "A",
			Name:    "app.org2.example.com",
			Content: "6.6.6.6",
		},
	})
	if err == nil {
		t.Errorf("updateRecord: expected error when Org 1 updates record in Org 2 domain, got nil")
	}
	for _, up := range fake.updated {
		if up.ID == "rec-org2-1" {
			t.Fatalf("updateRecord: Org 2 record was updated by Org 1")
		}
	}

	_, err = p.deleteRecord(ctx, &DeleteRecordInput{
		Ctx: ctxOrg1Admin,
		ID:  dom2.ID,
		RID: "rec-org2-1",
	})
	if err == nil {
		t.Errorf("deleteRecord: expected error when Org 1 deletes record in Org 2 domain, got nil")
	}
	for _, del := range fake.deleted {
		if del == "rec-org2-1" {
			t.Fatalf("deleteRecord: Org 2 record was deleted by Org 1")
		}
	}

	ddnsListOut, err := p.listDDNSTokens(ctx, &listDDNSTokensInput{Ctx: ctxOrg1Admin})
	if err != nil {
		t.Fatalf("listDDNSTokens: %v", err)
	}
	if len(ddnsListOut.Body) != 1 || ddnsListOut.Body[0].ID != tok1.ID {
		t.Errorf("listDDNSTokens: Org 1 saw DDNS tokens %+v, want only tok1 (%d)", ddnsListOut.Body, tok1.ID)
	}

	createTokIn := &createDDNSTokenInput{
		Ctx: ctxOrg1Admin,
	}
	createTokIn.Body.DomainID = dom2.ID
	createTokIn.Body.RecordName = "rogue-ddns"
	createTokIn.Body.RecordType = "A"
	createTokIn.Body.Label = "Rogue Token"

	_, err = p.createDDNSToken(ctx, createTokIn)
	if err == nil {
		t.Errorf("createDDNSToken: expected error when Org 1 creates token for Org 2 domain, got nil")
	}
	db.Model(&DDNSToken{}).Where("record_name = ?", "rogue-ddns").Count(&count)
	if count != 0 {
		t.Fatalf("createDDNSToken: DDNS token created against Org 2 domain")
	}

	_, err = p.deleteDDNSToken(ctx, &deleteDDNSTokenInput{
		Ctx: ctxOrg1Admin,
		ID:  tok2.ID,
	})
	if err == nil {
		t.Errorf("deleteDDNSToken: expected error when Org 1 deletes Org 2 token, got nil")
	}
	db.Model(&DDNSToken{}).Where("id = ?", tok2.ID).Count(&count)
	if count == 0 {
		t.Fatalf("deleteDDNSToken: Org 2 DDNS token was deleted by Org 1")
	}

	_, err = p.updateDomain(ctx, &UpdateDomainInput{Ctx: ctxOrg2Admin, ID: dom1.ID, Body: domainDTO{Note: "x"}})
	if err == nil {
		t.Errorf("updateDomain: expected error for Org 2 updating Org 1 domain")
	}

	_, err = p.deleteDomain(ctx, &DeleteDomainInput{Ctx: ctxOrg2Admin, ID: dom1.ID})
	if err == nil {
		t.Errorf("deleteDomain: expected error for Org 2 deleting Org 1 domain")
	}

	createTokIn2 := &createDDNSTokenInput{Ctx: ctxOrg2Admin}
	createTokIn2.Body.DomainID = dom1.ID
	createTokIn2.Body.RecordName = "rogue-ddns-2"
	_, err = p.createDDNSToken(ctx, createTokIn2)
	if err == nil {
		t.Errorf("createDDNSToken: expected error for Org 2 on Org 1 domain")
	}

	_, err = p.deleteDDNSToken(ctx, &deleteDDNSTokenInput{Ctx: ctxOrg2Admin, ID: tok1.ID})
	if err == nil {
		t.Errorf("deleteDDNSToken: expected error for Org 2 deleting Org 1 token")
	}
}
