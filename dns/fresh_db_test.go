package dns

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var memDBCounter atomic.Uint64

func freshMemDB(t *testing.T) *gorm.DB {
	t.Helper()
	n := memDBCounter.Add(1)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:memdb-%d?mode=memory&cache=shared", n)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(append((&Plugin{}).Models(), &testSetting{}, &testOrgSlugHistory{})...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func setupFreshTestDB(t *testing.T) (*Plugin, func(req *http.Request) huma.Context) {
	t.Helper()
	p := New()
	p.db = freshMemDB(t)
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
	mkCtx := func(r *http.Request) huma.Context {
		if r.Header.Get("X-Org-ID") == "" {
			r.Header.Set("X-Org-ID", "1")
		}
		return humago.NewContext(nil, r, httptest.NewRecorder())
	}
	return p, mkCtx
}

func setupFreshGateTestDB(t *testing.T) (*Plugin, func(role string) huma.Context) {
	t.Helper()
	p := New()
	p.db = freshMemDB(t)
	p.vaultSvc = newMockVaultService()
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
