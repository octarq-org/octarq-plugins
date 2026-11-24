package maillink

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	linkscontract "github.com/octarq-org/octarq-plugins/links/contract"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type mockSession struct {
	orgID func(r *http.Request) uint
}

func (s *mockSession) UserID(r *http.Request) uint                     { return 1 }
func (s *mockSession) OrgID(r *http.Request) uint                      { return s.orgID(r) }
func (s *mockSession) PersonalOrgID(r *http.Request) uint              { return 1 }
func (s *mockSession) OrgRole(r *http.Request) string                  { return "admin" }
func (s *mockSession) RequireRole(r *http.Request, min string) bool    { return true }
func (s *mockSession) RequirePerm(r *http.Request, p, min string) bool { return true }
func (s *mockSession) IsInstanceAdmin(r *http.Request) bool            { return false }
func (s *mockSession) RevokeUserOrgSessions(u, o uint) int             { return 0 }

type mockHost struct {
	session *mockSession
	db      *gorm.DB
}

func (h *mockHost) Session() plugin.HostSession                                { return h.session }
func (h *mockHost) Crypto() plugin.CryptoVault                                 { return nil }
func (h *mockHost) Settings() plugin.SettingsStore                             { return nil }
func (h *mockHost) Events() plugin.EventSpine                                  { return nil }
func (h *mockHost) Hosts() plugin.HostRegistry                                 { return plugin.NoopHostRegistry() }
func (h *mockHost) TrustProxy() bool                                           { return false }
func (h *mockHost) SpendLedger() plugin.SpendLedger                            { return nil }
func (h *mockHost) HarnessSpec(context.Context, uint) (json.RawMessage, error) { return nil, nil }
func (h *mockHost) TenantDB(orgID uint) *plugin.TenantDB {
	if h.db == nil || orgID == 0 {
		return nil
	}
	tdb, _ := plugin.NewTenantDB(h.db, orgID)
	return tdb
}
func (h *mockHost) LookupCredential(ctx context.Context, orgID uint, query plugin.CredentialQuery) (*plugin.Credential, error) {
	return nil, plugin.ErrCredentialResolverUnavailable
}

func TestMailLink_Validate(t *testing.T) {
	p := &Plugin{}
	if err := p.Validate(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil host")
	}
	if err := p.Validate(context.Background(), &mockHost{}); err != nil {
		t.Fatalf("unexpected error for valid host: %v", err)
	}
}

func TestMailLink_FailClosedOnZeroOrgID(t *testing.T) {
	p := &Plugin{}
	host := &mockHost{
		session: &mockSession{
			orgID: func(r *http.Request) uint { return 0 },
		},
	}
	p.host = host

	rec := httptest.NewRecorder()
	p.recent(rec, httptest.NewRequest("GET", "/api/maillink/recent", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for recent with zero orgID, got %d", rec.Code)
	}
}

func TestMailLink_ListAndTenantDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	_ = db.AutoMigrate(&MailLink{})

	p := &Plugin{}
	host := &mockHost{
		db: db,
		session: &mockSession{
			orgID: func(r *http.Request) uint { return 1 },
		},
	}
	p.host = host

	tdb1 := host.TenantDB(1)
	_ = tdb1.Create(&MailLink{OrgID: 1, Slug: "slug-1", Target: "https://example.com/1"})
	tdb2 := host.TenantDB(2)
	_ = tdb2.Create(&MailLink{OrgID: 2, Slug: "slug-2", Target: "https://example.com/2"})

	links1 := p.list(context.Background(), 1, 10)
	if len(links1) != 1 || links1[0].Slug != "slug-1" {
		t.Fatalf("expected 1 link for org 1, got %+v", links1)
	}

	links2 := p.list(context.Background(), 2, 10)
	if len(links2) != 1 || links2[0].Slug != "slug-2" {
		t.Fatalf("expected 1 link for org 2, got %+v", links2)
	}
}

func TestMailLink_MountWithoutHTTPSurface(t *testing.T) {
	p := &Plugin{}
	p.Mount(nil, &plugin.Context{})
}

func TestOnEventConsumesMailInboundContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&MailLink{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	reg := plugin.NewRegistry()
	creator := &fakeLinkCreator{slug: "short-1"}
	reg.Provide(linkscontract.ServiceCreate, linkscontract.Creator(creator))

	p := &Plugin{
		ctx: &plugin.Context{Lookup: reg.Lookup},
		host: &mockHost{
			db:      db,
			session: &mockSession{orgID: func(r *http.Request) uint { return 1 }},
		},
	}

	const payload = `{"id":7,"mailboxId":8,"orgId":3,"from":"sender@example.com","to":"user@example.net","subject":"Hello","text":"Read https://example.com/path today","html":"<p>Read <a href=\"https://example.com/path\">this</a></p>","receivedAt":"2026-01-02T03:04:05Z"}`
	p.onEvent("mail.inbound", json.RawMessage(payload))

	if creator.calls != 1 {
		t.Fatalf("link creator called %d times, want 1", creator.calls)
	}
	if creator.orgID != 3 || creator.target != "https://example.com/path" {
		t.Fatalf("creator got org=%d target=%q", creator.orgID, creator.target)
	}

	var links []MailLink
	if err := db.Find(&links).Error; err != nil {
		t.Fatalf("load links: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("stored %d mail links, want 1", len(links))
	}
	got := links[0]
	if got.OrgID != 3 || got.Slug != "short-1" || got.Target != "https://example.com/path" {
		t.Fatalf("stored link = %+v", got)
	}
	if got.Subject != "Hello" || got.From != "sender@example.com" {
		t.Fatalf("stored link did not keep the mail fields: %+v", got)
	}

	p.onEvent("mail.outbound", json.RawMessage(payload))

	if creator.calls != 1 {
		t.Fatalf("link creator called %d times after a different topic, want 1", creator.calls)
	}
	var count int64
	if err := db.Model(&MailLink{}).Count(&count).Error; err != nil {
		t.Fatalf("count links: %v", err)
	}
	if count != 1 {
		t.Fatalf("stored %d mail links after a different topic, want 1", count)
	}
}

type fakeLinkCreator struct {
	slug   string
	calls  int
	orgID  uint
	target string
}

func (f *fakeLinkCreator) CreateLink(_ context.Context, orgID uint, targetURL string) (string, error) {
	f.calls++
	f.orgID = orgID
	f.target = targetURL
	return f.slug, nil
}
