package dns

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func TestCharacterization_PluginContract(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}

	if p.Name() != "dns" {
		t.Errorf("expected plugin name 'dns', got %q", p.Name())
	}

	desc := p.Describe()
	if desc.Title != "Domains & DNS" {
		t.Errorf("unexpected plugin title: %q", desc.Title)
	}

	menus := p.Menus()
	if len(menus) == 0 {
		t.Error("expected at least one menu item for dns plugin")
	}

	docs := p.HelpDocsFS()
	if docs == nil {
		t.Fatal("expected non-nil HelpDocsFS")
	}
	entries, err := fs.ReadDir(docs, "docs")
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected readable docs directory with pages, err=%v", err)
	}
}

func TestCharacterization_FailClosedTenantBoundary(t *testing.T) {
	p, _ := setupFullDNSTestDB(t)

	req0 := httptest.NewRequest(http.MethodGet, "/api/dns/domains", nil)
	req0.Header.Set("X-Org-ID", "0")
	if id := p.orgID(req0); id != 0 {
		t.Errorf("expected orgID 0, got %d", id)
	}

	mgr := p.DNSManager()
	if mgr == nil {
		t.Fatal("DNSManager() returned nil")
	}

	_, err := mgr.List(context.Background(), 0, 999)
	if err == nil {
		t.Error("List with orgID 0 must fail closed with error")
	}
}

func TestCharacterization_DDNSStateSecurity(t *testing.T) {
	p, mkCtx := setupFullDNSTestDB(t)
	ctx := context.Background()

	dom := Domain{
		OrgID:             1,
		Name:              "ddns-char.example.com",
		ProviderAccountID: 1,
	}
	if err := p.db.Create(&dom).Error; err != nil {
		t.Fatalf("create test domain: %v", err)
	}

	in := &createDDNSTokenInput{}
	req := httptest.NewRequest(http.MethodPost, "/api/dns/ddns/tokens", nil)
	req.Header.Set("X-Org-ID", "1")
	in.Ctx = mkCtx(req)
	in.Body.DomainID = dom.ID
	in.Body.RecordName = "home.ddns-char.example.com"
	in.Body.RecordType = "A"
	in.Body.Label = "Characterization Token"

	out, err := p.createDDNSToken(ctx, in)
	if err != nil {
		t.Fatalf("createDDNSToken error: %v", err)
	}

	plainSecret := out.Body.Secret
	if plainSecret == "" {
		t.Fatal("expected non-empty plain secret in creation response")
	}

	var stored DDNSToken
	if err := p.db.Where("domain_id = ?", dom.ID).First(&stored).Error; err != nil {
		t.Fatalf("fetch stored token: %v", err)
	}
	if stored.TokenHash == plainSecret {
		t.Fatal("CRITICAL: DDNS secret stored in plain text!")
	}
	expectedHash := hashDDNSSecret(plainSecret)
	if stored.TokenHash != expectedHash {
		t.Fatalf("expected stored hash %q, got %q", expectedHash, stored.TokenHash)
	}

	badReq := httptest.NewRequest(http.MethodGet, "/api/dns/ddns/update?token=invalid_key_random&ip=1.2.3.4", nil)
	rec := httptest.NewRecorder()
	badCtx := humago.NewContext(nil, badReq, rec)
	_, _ = p.updateDDNS(context.Background(), &updateDDNSInput{
		Token: "invalid_key_random",
		IP:    "1.2.3.4",
		Ctx:   badCtx,
	})
	if rec.Body.String() != "badauth" {
		t.Errorf("expected DynDNS protocol 'badauth' response for invalid token, got %q", rec.Body.String())
	}
}
