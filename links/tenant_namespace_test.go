package links

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openTenantTestPlugin(t *testing.T) (*gorm.DB, *Plugin) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&Link{}, &LinkEvent{}, &testSetting{}, &testDomain{}, &testProviderAccount{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	p := New()
	p.db = db
	p.auth.OrgID = func(r *http.Request) uint {
		if val := r.Header.Get("X-Org-ID"); val != "" {
			var id uint
			fmt.Sscanf(val, "%d", &id)
			return id
		}
		return 1
	}
	return db, p
}

func TestProvisionedTenantSubdomainIsLinkHost(t *testing.T) {
	db, p := openTenantTestPlugin(t)
	db.Create(&testSetting{Key: "base_domain", Value: "app.example.com"})

	if err := db.Create(&testDomain{
		OrgID:     7,
		Name:      "acme7x.app.example.com",
		ForLink:   true,
		LinkHosts: hostlist.HostList{{Host: "acme7x.app.example.com", Enabled: true}},
	}).Error; err != nil {
		t.Fatalf("seed tenant domain: %v", err)
	}
	if !p.ownsHost(7, "acme7x.app.example.com") {
		t.Fatal("tenant subdomain is not a usable link host for its org")
	}
	if p.ownsHost(1, "acme7x.app.example.com") {
		t.Fatal("tenant subdomain is visible to a different org")
	}
}

func TestEmptyHostRejectedWhenInstanceMultiTenant(t *testing.T) {
	db, p := openTenantTestPlugin(t)
	db.Create(&testSetting{Key: "base_domain", Value: "app.example.com"})
	db.Create(&testDomain{
		OrgID:     1,
		Name:      "acme.app.example.com",
		ForLink:   true,
		LinkHosts: hostlist.HostList{{Host: "acme.app.example.com", Enabled: true}},
	})

	ctx := context.Background()

	{
		req := httptest.NewRequest(http.MethodPost, "/api/links", nil)
		req.Header.Set("X-Org-ID", "1")
		input := &CreateLinkInput{
			Ctx:  humago.NewContext(nil, req, httptest.NewRecorder()),
			Body: linkDTO{Host: "", Slug: "naked", Target: "https://t.example/x"},
		}
		_, err := p.createLink(ctx, input)
		if err == nil || !strings.Contains(err.Error(), "host is required") {
			t.Fatalf("createLink(host='') err = %v, want 400 host-required", err)
		}
	}

	{
		req := httptest.NewRequest(http.MethodPost, "/api/links/quick", nil)
		req.Header.Set("X-Org-ID", "1")
		input := &QuickCreateLinkInput{
			Ctx:  humago.NewContext(nil, req, httptest.NewRecorder()),
			Body: QuickCreateLinkBody{URL: "https://t.example/y"},
		}
		_, err := p.quickCreateLink(ctx, input)
		if err == nil || !strings.Contains(err.Error(), "host is required") {
			t.Fatalf("quickCreateLink(host='') err = %v, want 400 host-required", err)
		}
	}

	{
		req := httptest.NewRequest(http.MethodPost, "/api/links", nil)
		req.Header.Set("X-Org-ID", "1")
		create := &CreateLinkInput{
			Ctx:  humago.NewContext(nil, req, httptest.NewRecorder()),
			Body: linkDTO{Host: "acme.app.example.com", Slug: "homed", Target: "https://t.example/homed"},
		}
		out, err := p.createLink(ctx, create)
		if err != nil {
			t.Fatalf("seed link: %v", err)
		}
		reqUp := httptest.NewRequest(http.MethodPut, "/api/links/1", nil)
		reqUp.Header.Set("X-Org-ID", "1")
		upd := &UpdateLinkInput{
			Ctx:  humago.NewContext(nil, reqUp, httptest.NewRecorder()),
			ID:   out.Body.ID,
			Body: linkDTO{Host: "", Slug: "homed", Target: "https://t.example/homed"},
		}
		_, err = p.updateLink(ctx, upd)
		if err == nil || !strings.Contains(err.Error(), "host is required") {
			t.Fatalf("updateLink(host='') err = %v, want 400 host-required", err)
		}
	}
}

func TestEmptyHostStillAllowedWithoutBase(t *testing.T) {
	db, p := openTenantTestPlugin(t)
	t.Setenv("OCTARQ_BASE_DOMAIN", "")
	db.Create(&testDomain{
		OrgID:     1,
		Name:      "mybrand.com",
		ForLink:   true,
		LinkHosts: hostlist.HostList{{Host: "go.mybrand.com", Enabled: true}},
	})

	ctx := context.Background()
	req := httptest.NewRequest(http.MethodPost, "/api/links", nil)
	req.Header.Set("X-Org-ID", "1")
	input := &CreateLinkInput{
		Ctx:  humago.NewContext(nil, req, httptest.NewRecorder()),
		Body: linkDTO{Host: "", Slug: "legacy", Target: "https://t.example/legacy"},
	}
	out, err := p.createLink(ctx, input)
	if err != nil {
		t.Fatalf("createLink(host='') without base domain err = %v, want success", err)
	}
	if out.Body.Host != "" {
		t.Fatalf("got host %q, want empty host stored", out.Body.Host)
	}
}
