package links

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"github.com/octarq-org/octarq/server/plugin"
)

func TestFinding1_DeclarativeLinkBypassesHostAndQuota(t *testing.T) {
	t.Parallel()
	p, mkCtx := setupFullLinksTestDB(t)
	ctxOrg := plugin.WithOrgID(context.Background(), 1)

	p.db.Create(&testSetting{Key: "base_domain", Value: "octarq.test"})
	p.db.Create(&testDomain{
		OrgID:   1,
		Name:    "mybrand.com",
		ForLink: true,
		LinkHosts: hostlist.HostList{
			{Host: "go.mybrand.com", Enabled: true},
		},
	})

	if !p.linkHostRequired(1) {
		t.Fatal("expected linkHostRequired to be true for org 1")
	}

	reqNormal := httptest.NewRequest(http.MethodPost, "/api/links", nil)
	_, errNormal := p.createLink(context.Background(), &CreateLinkInput{
		Ctx:  mkCtx(reqNormal),
		Body: linkDTO{Slug: "normal-hostless", Target: "https://example.com"},
	})
	if errNormal == nil {
		t.Errorf("expected createLink to reject hostless link when linkHostRequired=true")
	}

	outHostless, errDecl := p.createDeclarativeLink(ctxOrg, DeclarativeLinkInput{
		Slug:        "decl-hostless",
		Destination: "https://example.com",
	})
	if errDecl != nil {
		t.Logf("createDeclarativeLink returned error: %v", errDecl)
	} else {
		t.Logf("[CONFIRMED Finding 1 Bug A] createDeclarativeLink created hostless link ID=%d when linkHostRequired is true", outHostless.ID)
		var created Link
		p.db.First(&created, outHostless.ID)
		if created.Host != "" {
			t.Errorf("expected Host to be empty, got %q", created.Host)
		}
	}

	withQuotaChecker(p, fakeQuotaChecker{err: plugin.ErrQuotaExceeded})

	reqQuota := httptest.NewRequest(http.MethodPost, "/api/links", nil)
	_, errQuotaNormal := p.createLink(context.Background(), &CreateLinkInput{
		Ctx:  mkCtx(reqQuota),
		Body: linkDTO{Slug: "quota-test", Target: "https://example.com", Host: "go.mybrand.com"},
	})
	if errQuotaNormal == nil {
		t.Errorf("expected normal createLink to be blocked by quota")
	}

	outQuota, errDeclQuota := p.createDeclarativeLink(ctxOrg, DeclarativeLinkInput{
		Slug:        "decl-quota-bypass",
		Destination: "https://example.com",
	})
	if errDeclQuota != nil {
		t.Logf("createDeclarativeLink returned error on quota: %v", errDeclQuota)
	} else {
		t.Logf("[CONFIRMED Finding 1 Bug B] createDeclarativeLink bypassed quota check and created link ID=%d", outQuota.ID)
	}
}

func TestFinding3_PasswordGateMethodGetExposesPlaintext(t *testing.T) {
	t.Parallel()
	s := newTestEngine(t)

	link := &Link{
		Slug:     "pw-secret",
		Target:   "https://target.example.com",
		Enabled:  true,
		Password: "SuperSecretPassword123!",
	}
	s.db.Create(link)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pw-secret", nil)
	s.Handle(rec, req, link)

	body := rec.Body.String()
	if !strings.Contains(body, `method="post"`) {
		t.Errorf("expected password gate to render method=\"post\", got: %s", body)
	}
	if strings.Contains(body, `method="get"`) {
		t.Errorf("password gate should not render method=\"get\" after fix")
	}
	if !strings.Contains(body, `name="referrer"`) {
		t.Errorf("expected referrer meta tag in password gate")
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("expected Referrer-Policy: no-referrer on 401 gate, got %q", rec.Header().Get("Referrer-Policy"))
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for gate, got %d", rec.Code)
	}

	recRedirect := httptest.NewRecorder()
	reqRedirect := httptest.NewRequest(http.MethodGet, "/pw-secret?pw=SuperSecretPassword123!", nil)
	s.Handle(recRedirect, reqRedirect, link)
	if recRedirect.Code != http.StatusFound {
		t.Fatalf("expected 302 Found redirect, got %d", recRedirect.Code)
	}
	if recRedirect.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("expected Referrer-Policy: no-referrer on redirect, got %q", recRedirect.Header().Get("Referrer-Policy"))
	}
	recPost := httptest.NewRecorder()
	form := strings.NewReader("pw=SuperSecretPassword123!")
	reqPost := httptest.NewRequest(http.MethodPost, "/pw-secret", form)
	reqPost.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.Handle(recPost, reqPost, link)
	if recPost.Code != http.StatusFound {
		t.Fatalf("expected 302 Found via POST, got %d", recPost.Code)
	}
}

func TestFinding5_CrossPluginCreateLinkQuotaBypass(t *testing.T) {
	t.Parallel()
	p, _ := setupFullLinksTestDB(t)

	withQuotaChecker(p, fakeQuotaChecker{err: plugin.ErrQuotaExceeded})

	slug, err := p.CreateLink(context.Background(), 1, "https://example.com/from-cross-plugin")
	if err != nil {
		t.Logf("CreateLink returned error: %v", err)
	} else {
		t.Logf("[CONFIRMED Finding 5] CreateLink bypassed quota check and created link with slug: %s", slug)
		var count int64
		p.db.Model(&Link{}).Where("slug = ?", slug).Count(&count)
		if count != 1 {
			t.Errorf("expected 1 link row, got %d", count)
		}
	}
}

func TestFinding7_ClickLimitCacheDelay(t *testing.T) {
	t.Parallel()

	cacheStore := make(map[string]Link)
	ctx := &plugin.Context{
		CacheGet: func(_ context.Context, key string, val any) bool {
			if l, ok := cacheStore[key]; ok {
				*(val.(*Link)) = l
				return true
			}
			return false
		},
		CacheSet: func(_ context.Context, key string, val any, ttl time.Duration) error {
			if l, ok := val.(*Link); ok {
				cacheStore[key] = *l
			}
			return nil
		},
	}

	p, _ := setupFullLinksTestDB(t)
	eng := NewEngine(p.db, ctx)

	link := &Link{
		OrgID:      1,
		Slug:       "limited-link",
		Target:     "https://example.com",
		Enabled:    true,
		ClickLimit: 2,
		Clicks:     0,
	}
	p.db.Create(link)

	l1, ok := eng.Lookup("h", "limited-link")
	if !ok || l1 == nil {
		t.Fatal("expected Lookup to succeed")
		return
	}

	p.db.Model(&Link{}).Where("id = ?", link.ID).Update("clicks", 100)

	var dbLink Link
	p.db.First(&dbLink, link.ID)
	if dbLink.Clicks != 100 {
		t.Fatalf("expected DB clicks to be 100, got %d", dbLink.Clicks)
	}

	l2, ok2 := eng.Lookup("h", "limited-link")
	if !ok2 || l2 == nil {
		t.Fatal("expected Lookup to hit cache")
		return
	}

	if l2.Clicks != 0 {
		t.Errorf("expected cached clicks to be 0, got %d", l2.Clicks)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/limited-link", nil)
	eng.Handle(rec, req, l2)

	switch rec.Code {
	case http.StatusNotFound:
		t.Errorf("Handle blocked link (expected bug: Handle redirects because cached clicks=0)")
	case http.StatusFound:
		t.Logf("[CONFIRMED Finding 7] Handle redirected (302 Found) despite DB clicks=100 > ClickLimit=2 due to 1-hour cache TTL")
	}
}
