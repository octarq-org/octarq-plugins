package maillink

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type mockSession struct {
	orgID func(r *http.Request) uint
}

func (s *mockSession) UserID(r *http.Request) uint                     { return 1 }
func (s *mockSession) OrgID(r *http.Request) uint                      { return s.orgID(r) }
func (s *mockSession) OrgRole(r *http.Request) string                  { return "admin" }
func (s *mockSession) RequireRole(r *http.Request, min string) bool    { return true }
func (s *mockSession) RequirePerm(r *http.Request, p, min string) bool { return true }
func (s *mockSession) IsInstanceAdmin(r *http.Request) bool            { return false }
func (s *mockSession) RevokeUserOrgSessions(u, o uint) int             { return 0 }

type mockHost struct {
	session *mockSession
	db      *gorm.DB
}

func (h *mockHost) Session() plugin.HostSession    { return h.session }
func (h *mockHost) Crypto() plugin.CryptoVault     { return nil }
func (h *mockHost) Settings() plugin.SettingsStore { return nil }
func (h *mockHost) Events() plugin.EventSpine      { return nil }
func (h *mockHost) TenantDB(orgID uint) *plugin.TenantDB {
	if h.db == nil || orgID == 0 {
		return nil
	}
	tdb, _ := plugin.NewTenantDB(h.db, orgID)
	return tdb
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

	// Seed rows for org 1 and org 2
	tdb1 := host.TenantDB(1)
	_ = tdb1.Create(&MailLink{OrgID: 1, Slug: "slug-1", Target: "https://example.com/1"})
	tdb2 := host.TenantDB(2)
	_ = tdb2.Create(&MailLink{OrgID: 2, Slug: "slug-2", Target: "https://example.com/2"})

	// Query for org 1
	links1 := p.list(context.Background(), 1, 10)
	if len(links1) != 1 || links1[0].Slug != "slug-1" {
		t.Fatalf("expected 1 link for org 1, got %+v", links1)
	}

	// Query for org 2
	links2 := p.list(context.Background(), 2, 10)
	if len(links2) != 1 || links2[0].Slug != "slug-2" {
		t.Fatalf("expected 1 link for org 2, got %+v", links2)
	}
}
