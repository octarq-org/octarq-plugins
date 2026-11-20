package dns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq-plugins/dns/provider"
	"gorm.io/gorm"
)

func setupGateTestDB(t *testing.T) (*Plugin, func(role string) huma.Context) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	p := New()
	p.db = db
	p.vaultSvc = newMockVaultService()
	if err := db.AutoMigrate(append(p.Models(), &testSetting{}, &testOrgSlugHistory{})...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	p.audit = func(r *http.Request, action, targetType string, targetID uint, meta map[string]any) {}
	p.orgID = func(r *http.Request) uint {
		var id uint
		fmt.Sscanf(r.Header.Get("X-Org-ID"), "%d", &id)
		if id == 0 {
			id = 1
		}
		return id
	}
	p.requireRole = func(r *http.Request, role string) bool {
		return r.Header.Get("X-Role") == "admin"
	}

	mkCtx := func(role string) huma.Context {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("X-Org-ID", "1")
		r.Header.Set("X-Role", role)
		return humago.NewContext(nil, r, httptest.NewRecorder())
	}
	return p, mkCtx
}

func TestCreateRecordRoleGate(t *testing.T) {
	t.Parallel()
	p, mkCtx := setupGateTestDB(t)

	acc := ProviderAccount{OrgID: 1, Name: "CF", Type: "cloudflare"}
	p.db.Create(&acc)
	dom := Domain{OrgID: 1, Name: "gate-test.com", ProviderAccountID: acc.ID, ZoneID: "zone-abc"}
	p.db.Create(&dom)

	in := &CreateRecordInput{
		ID:   dom.ID,
		Body: provider.Record{Type: "A", Content: "1.2.3.4", Name: "gate-test.com"},
	}

	in.Ctx = mkCtx("member")
	_, err := p.createRecord(context.Background(), in)
	if err == nil {
		t.Fatal("createRecord: expected 403 for member, got nil error")
		return
	}
	if he, ok := err.(huma.StatusError); !ok || he.GetStatus() != 403 {
		t.Fatalf("createRecord: expected 403, got %v", err)
	}

	in.Ctx = mkCtx("admin")
	_, err = p.createRecord(context.Background(), in)
	if err == nil {
		t.Fatal("createRecord: expected provider error for admin with bad creds, got nil")
		return
	}
	if he, ok := err.(huma.StatusError); ok && he.GetStatus() == 403 {
		t.Fatalf("createRecord: admin must not be refused with 403, got %v", err)
	}
}

func TestUpdateRecordRoleGate(t *testing.T) {
	t.Parallel()
	p, mkCtx := setupGateTestDB(t)

	acc := ProviderAccount{OrgID: 1, Name: "CF2", Type: "cloudflare"}
	p.db.Create(&acc)
	dom := Domain{OrgID: 1, Name: "gate-upd.com", ProviderAccountID: acc.ID, ZoneID: "zone-upd"}
	p.db.Create(&dom)

	in := &UpdateRecordInput{
		ID:   dom.ID,
		RID:  "rec-123",
		Body: provider.Record{Type: "A", Content: "5.6.7.8", Name: "gate-upd.com"},
	}

	in.Ctx = mkCtx("member")
	_, err := p.updateRecord(context.Background(), in)
	if err == nil {
		t.Fatal("updateRecord: expected 403 for member, got nil error")
		return
	}
	if he, ok := err.(huma.StatusError); !ok || he.GetStatus() != 403 {
		t.Fatalf("updateRecord: expected 403, got %v", err)
	}

	in.Ctx = mkCtx("admin")
	_, err = p.updateRecord(context.Background(), in)
	if err == nil {
		t.Fatal("updateRecord: expected provider error for admin with bad creds, got nil")
		return
	}
	if he, ok := err.(huma.StatusError); ok && he.GetStatus() == 403 {
		t.Fatalf("updateRecord: admin must not be refused with 403, got %v", err)
	}
}

func TestProviderErrSanitization(t *testing.T) {
	t.Parallel()
	p := &Plugin{}

	marker := "CF-RAY:abc123xyz-secret"
	rawErr := errors.New("cloudflare: create record: 400 Bad Request: " + marker)

	returned := p.providerErr("create record", rawErr)
	if returned == nil {
		t.Fatal("providerErr returned nil")
	}

	if strings.Contains(returned.Error(), marker) {
		t.Errorf("provider raw text leaked into response: %v", returned.Error())
	}
	if !strings.Contains(returned.Error(), "create record") {
		t.Errorf("response should contain the action name, got: %v", returned.Error())
	}
}
