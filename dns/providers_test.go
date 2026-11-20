package dns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/octarq-org/octarq/server/plugin"
)

var errBoom = errors.New("boom")

func setupProviderDB(t *testing.T) (*Plugin, func(req *http.Request) huma.Context) {
	t.Helper()
	return setupFreshTestDB(t)
}

var auditCalls []string

func collectAudit(p *Plugin) {
	auditCalls = nil
	p.audit = func(r *http.Request, action, targetType string, targetID uint, meta map[string]any) {
		auditCalls = append(auditCalls, action)
	}
}

func mkAdmin(t *testing.T, mkCtx func(*http.Request) huma.Context) huma.Context {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Org-ID", "1")
	req.Header.Set("X-Role", "admin")
	return mkCtx(req)
}

func TestProviderAccountCRUDFlow(t *testing.T) {
	p, mkCtx := setupProviderDB(t)
	ctx := context.Background()
	collectAudit(p)

	created, err := p.createProviderAccount(ctx, &CreateProviderAccountInput{
		Ctx:  mkAdmin(t, mkCtx),
		Body: providerAccountDTO{Name: "  Personal CF  ", Type: "cloudflare", Config: map[string]any{"apiToken": "tok"}},
	})
	if err != nil {
		t.Fatalf("createProviderAccount: %v", err)
	}
	if created.Body.ID == 0 || created.Body.Name != "Personal CF" || !created.Body.HasCredentials || created.Body.VaultItemID == 0 {
		t.Fatalf("created account wrong: %+v", created.Body)
	}
	if len(auditCalls) != 1 || auditCalls[0] != "provider.create" {
		t.Errorf("expected provider.create audit, got %v", auditCalls)
	}
	var stored ProviderAccount
	if err := p.db.First(&stored, created.Body.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	sec, err := p.vaultSvc.GetSecret(ctx, 1, stored.VaultItemID)
	if err != nil || sec.Payload["apiToken"] != "tok" {
		t.Errorf("vault secret = %+v, err=%v", sec, err)
	}

	p.db.Create(&ProviderAccount{OrgID: 1, Name: "No Creds", Type: "dnspod"})
	out, err := p.listProviderAccounts(ctx, &ListProviderAccountsInput{Ctx: mkAdmin(t, mkCtx)})
	if err != nil {
		t.Fatalf("listProviderAccounts: %v", err)
	}
	if len(out.Body) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(out.Body))
	}
	creds := map[uint]bool{}
	for _, a := range out.Body {
		creds[a.ID] = a.HasCredentials
	}
	if !creds[created.Body.ID] || creds[created.Body.ID+1] {
		t.Errorf("HasCredentials flags wrong: %v", creds)
	}

	updated, err := p.updateProviderAccount(ctx, &UpdateProviderAccountInput{
		Ctx:  mkAdmin(t, mkCtx),
		ID:   created.Body.ID,
		Body: providerAccountDTO{Name: "Renamed", Config: map[string]any{"apiToken": "new"}},
	})
	if err != nil {
		t.Fatalf("updateProviderAccount: %v", err)
	}
	if updated.Body.Name != "Renamed" || !updated.Body.HasCredentials {
		t.Errorf("updated account = %+v", updated.Body)
	}
	secUpdated, err := p.vaultSvc.GetSecret(ctx, 1, updated.Body.VaultItemID)
	if err != nil || secUpdated.Payload["apiToken"] != "new" {
		t.Errorf("updated vault secret = %+v, err=%v", secUpdated, err)
	}
	if len(auditCalls) != 2 || auditCalls[1] != "provider.update" {
		t.Errorf("expected provider.update audit, got %v", auditCalls)
	}

	del, err := p.deleteProviderAccount(ctx, &DeleteProviderAccountInput{Ctx: mkAdmin(t, mkCtx), ID: created.Body.ID})
	if err != nil || !del.Body["ok"] {
		t.Fatalf("deleteProviderAccount: %v", err)
	}
	if len(auditCalls) != 3 || auditCalls[2] != "provider.delete" {
		t.Errorf("expected provider.delete audit, got %v", auditCalls)
	}
	var n int64
	p.db.Model(&ProviderAccount{}).Where("id = ?", created.Body.ID).Count(&n)
	if n != 0 {
		t.Errorf("account row still present after delete")
	}
	if _, err := p.vaultSvc.GetSecret(ctx, 1, stored.VaultItemID); !errors.Is(err, plugin.ErrSecretNotFound) {
		t.Errorf("expected vault secret to be deleted, got %v", err)
	}
}

func TestProviderAccountValidationAndGates(t *testing.T) {
	p, mkCtx := setupProviderDB(t)
	ctx := context.Background()
	acc := ProviderAccount{OrgID: 1, Name: "Other", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}

	member := httptest.NewRequest(http.MethodPost, "/", nil)
	member.Header.Set("X-Org-ID", "1")
	member.Header.Set("X-Role", "member")

	_, err := p.createProviderAccount(ctx, &CreateProviderAccountInput{
		Ctx:  mkAdmin(t, mkCtx),
		Body: providerAccountDTO{Name: "  ", Type: "cloudflare"},
	})
	if got := statusOf(t, err); got != http.StatusBadRequest {
		t.Fatalf("blank name: want 400, got %d", got)
	}

	_, err = p.createProviderAccount(ctx, &CreateProviderAccountInput{
		Ctx: mkCtx(member), Body: providerAccountDTO{Name: "n", Type: "t"},
	})
	if got := statusOf(t, err); got != http.StatusForbidden {
		t.Fatalf("member create: want 403, got %d", got)
	}

	noOrg := httptest.NewRequest(http.MethodPost, "/", nil)
	noOrg.Header.Set("X-Org-ID", "0")
	noOrg.Header.Set("X-Role", "admin")
	_, err = p.createProviderAccount(ctx, &CreateProviderAccountInput{
		Ctx: mkCtx(noOrg), Body: providerAccountDTO{Name: "n", Type: "t"},
	})
	if got := statusOf(t, err); got != http.StatusUnauthorized {
		t.Fatalf("org0 create: want 401, got %d", got)
	}

	_, err = p.createProviderAccount(ctx, &CreateProviderAccountInput{Body: providerAccountDTO{Name: "n", Type: "t"}})
	if got := statusOf(t, err); got != http.StatusInternalServerError {
		t.Fatalf("nil ctx: want 500, got %d", got)
	}

	other := ProviderAccount{OrgID: 2, Name: "Victim", Type: "cloudflare"}
	if err := p.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	foreign := httptest.NewRequest(http.MethodPut, "/", nil)
	foreign.Header.Set("X-Org-ID", "1")
	foreign.Header.Set("X-Role", "admin")
	_, err = p.updateProviderAccount(ctx, &UpdateProviderAccountInput{Ctx: mkCtx(foreign), ID: other.ID, Body: providerAccountDTO{Name: "hijack"}})
	if got := statusOf(t, err); got != http.StatusNotFound {
		t.Fatalf("cross-org update: want 404, got %d", got)
	}
	_, err = p.deleteProviderAccount(ctx, &DeleteProviderAccountInput{Ctx: mkCtx(foreign), ID: other.ID})
	if got := statusOf(t, err); got != http.StatusNotFound {
		t.Fatalf("cross-org delete: want 404, got %d", got)
	}
	var after ProviderAccount
	p.db.First(&after, other.ID)
	if after.Name != "Victim" {
		t.Errorf("cross-org attacker modified the victim row: %+v", after)
	}
}

func TestDeleteProviderAccountInUseByDomain(t *testing.T) {
	p, mkCtx := setupProviderDB(t)
	ctx := context.Background()
	acc := ProviderAccount{OrgID: 1, Name: "Used", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	p.db.Create(&Domain{OrgID: 1, Name: "used.com", ProviderAccountID: acc.ID})

	_, err := p.deleteProviderAccount(ctx, &DeleteProviderAccountInput{Ctx: mkAdmin(t, mkCtx), ID: acc.ID})
	if got := statusOf(t, err); got != http.StatusConflict {
		t.Fatalf("delete in-use account: want 409, got %d", got)
	}
	var n int64
	p.db.Model(&ProviderAccount{}).Where("id = ?", acc.ID).Count(&n)
	if n != 1 {
		t.Errorf("in-use account was deleted: count=%d", n)
	}
}

func TestProviderAccountVaultFailure(t *testing.T) {
	p, mkCtx := setupProviderDB(t)
	mv := p.vaultSvc.(*mockVaultService)
	mv.putErr = errBoom
	ctx := context.Background()

	_, err := p.createProviderAccount(ctx, &CreateProviderAccountInput{
		Ctx: mkAdmin(t, mkCtx), Body: providerAccountDTO{Name: "n", Type: "t", Config: map[string]any{"k": "v"}},
	})
	if got := statusOf(t, err); got != http.StatusInternalServerError {
		t.Fatalf("vault failure: want 500, got %d", got)
	}

	mv.putErr = nil
	created, err := p.createProviderAccount(ctx, &CreateProviderAccountInput{
		Ctx: mkAdmin(t, mkCtx), Body: providerAccountDTO{Name: "n", Type: "t"},
	})
	if err != nil {
		t.Fatalf("create without config: %v", err)
	}
	if created.Body.HasCredentials {
		t.Error("empty config must yield HasCredentials=false")
	}
}

func TestVaultService_DNSCredentialResolution(t *testing.T) {
	p, _ := setupFreshTestDB(t)
	p.orgID = func(r *http.Request) uint { return 10 }
	mv := p.vaultSvc.(*mockVaultService)

	meta, err := mv.PutSecret(context.Background(), plugin.PutSecretRequest{
		OrgID:          10,
		Scope:          plugin.VaultScopeOrg,
		CollectionSlug: "integrations",
		ItemType:       "integration",
		ProviderType:   "dns",
		Provider:       "cloudflare",
		Name:           "Vault CF Account",
		Payload:        map[string]any{"apiToken": "pro-token-xyz"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	if owned, err := p.ownsProviderAccount(req, meta.ID); !owned || err != nil {
		t.Errorf("expected ownsProviderAccount(%d) = true via Vault, got %v, %v", meta.ID, owned, err)
	}

	if owned, err := p.ownsProviderAccount(req, 999); owned || err != nil {
		t.Errorf("expected ownsProviderAccount(999) = false, got %v, %v", owned, err)
	}

	prov, err := p.providerFor(Domain{
		OrgID:             10,
		ProviderAccountID: meta.ID,
	})
	if err != nil {
		t.Fatalf("providerFor failed: %v", err)
	}
	if prov == nil {
		t.Fatal("expected non-nil provider from Vault")
	}

	p.vaultSvc = nil
	if owned, err := p.ownsProviderAccount(req, meta.ID); owned || !errors.Is(err, errProviderCredentials) {
		t.Errorf("expected missing Vault to surface as errProviderCredentials, got %v, %v", owned, err)
	}
	if _, err := p.providerFor(Domain{OrgID: 10, ProviderAccountID: meta.ID}); !errors.Is(err, errProviderCredentials) {
		t.Errorf("expected missing Vault to surface as errProviderCredentials, got %v", err)
	}

	mv.getErr = errors.New("database is locked")
	mv.metaErr = errors.New("database is locked")
	p.vaultSvc = mv
	if owned, err := p.ownsProviderAccount(req, meta.ID); owned || !errors.Is(err, errProviderCredentials) {
		t.Errorf("expected broken Vault to surface as errProviderCredentials, got %v, %v", owned, err)
	}
	if _, err := p.providerFor(Domain{OrgID: 10, ProviderAccountID: meta.ID}); !errors.Is(err, errProviderCredentials) {
		t.Errorf("expected broken Vault to surface as errProviderCredentials, got %v", err)
	}
	mv.getErr = nil
	mv.metaErr = nil

	emptyMeta, err := mv.PutSecret(context.Background(), plugin.PutSecretRequest{
		OrgID:        10,
		Scope:        plugin.VaultScopeOrg,
		ItemType:     "integration",
		ProviderType: "dns",
		Provider:     "cloudflare",
		Name:         "Empty CF",
		Payload:      nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.providerFor(Domain{OrgID: 10, ProviderAccountID: emptyMeta.ID}); !errors.Is(err, errProviderCredentials) {
		t.Errorf("expected an empty integration to surface as errProviderCredentials, got %v", err)
	}
}
