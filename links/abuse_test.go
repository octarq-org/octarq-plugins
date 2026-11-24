package links

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq/server/plugin"
)

func setupAbuseTestEnv(t *testing.T) (*Plugin, func(r *http.Request) huma.Context) {
	t.Helper()
	p, mkCtx := setupFullLinksTestDB(t)
	p.SetAbuseRateLimit(100, time.Hour)
	p.engine = NewEngine(p.db, mockCtx())
	return p, mkCtx
}

func TestSubmitAbuseSuccess(t *testing.T) {
	t.Parallel()
	p, mkCtx := setupAbuseTestEnv(t)
	ctx := context.Background()

	p.db.Create(&Link{
		OrgID:   1,
		Slug:    "promo",
		Target:  "https://example.com/promo",
		Enabled: true,
	})

	var notified bool
	var notifiedEvent string
	p.emit = func(ctx context.Context, payload plugin.NotificationPayload) error {
		notified = true
		notifiedEvent = payload.EventType
		if payload.Data["slug"] != "promo" {
			t.Errorf("notified slug = %v, want promo", payload.Data["slug"])
		}
		if _, ok := payload.Data["ip"]; ok {
			t.Errorf("notification contains plaintext ip")
		}
		return nil
	}

	req := httptest.NewRequest(http.MethodPost, "/abuse", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	in := &SubmitAbuseInput{
		Ctx: mkCtx(req),
		Body: SubmitAbuseInputBody{
			Slug:        "promo",
			Reason:      "spam",
			Description: "Spam link found in email",
		},
	}

	out, err := p.submitAbuse(ctx, in)
	if err != nil {
		t.Fatalf("submitAbuse: %v", err)
	}
	if !out.Body.OK || out.Body.ID == 0 {
		t.Fatalf("unexpected output: %+v", out)
	}

	var saved AbuseReport
	if err := p.db.First(&saved, out.Body.ID).Error; err != nil {
		t.Fatalf("failed to find saved report: %v", err)
	}
	if saved.Slug != "promo" || saved.Target != "https://example.com/promo" || saved.OrgID != 1 {
		t.Errorf("saved report = %+v", saved)
	}
	if saved.Status != "open" {
		t.Errorf("status = %q, want open", saved.Status)
	}
	if !notified || notifiedEvent != "abuse.report" {
		t.Errorf("notified = %v, event = %q", notified, notifiedEvent)
	}
}

func TestSubmitAbuseRateLimit(t *testing.T) {
	t.Parallel()
	p, mkCtx := setupAbuseTestEnv(t)
	ctx := context.Background()

	p.SetAbuseRateLimit(2, time.Hour)

	call := func() error {
		req := httptest.NewRequest(http.MethodPost, "/abuse", nil)
		req.RemoteAddr = "198.51.100.1:4567"
		in := &SubmitAbuseInput{
			Ctx: mkCtx(req),
			Body: SubmitAbuseInputBody{
				Slug:   "test",
				Reason: "other",
			},
		}
		_, err := p.submitAbuse(ctx, in)
		return err
	}

	if err := call(); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if err := call(); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	err := call()
	if err == nil {
		t.Fatal("expected rate limit error on 3rd call, got nil")
	}
	humaErr, ok := err.(huma.StatusError)
	if !ok || humaErr.GetStatus() != http.StatusTooManyRequests {
		t.Fatalf("status = %v, want 429", err)
	}
}

func TestSubmitAbuseValidation(t *testing.T) {
	t.Parallel()
	p, mkCtx := setupAbuseTestEnv(t)
	ctx := context.Background()

	t.Run("empty slug", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/abuse", nil)
		in := &SubmitAbuseInput{
			Ctx: mkCtx(req),
			Body: SubmitAbuseInputBody{
				Slug:   "   ",
				Reason: "spam",
			},
		}
		_, err := p.submitAbuse(ctx, in)
		if err == nil {
			t.Fatal("expected error for empty slug")
		}
		if se, ok := err.(huma.StatusError); !ok || se.GetStatus() != http.StatusBadRequest {
			t.Fatalf("got status %v, want 400", err)
		}
	})

	t.Run("invalid reason", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/abuse", nil)
		in := &SubmitAbuseInput{
			Ctx: mkCtx(req),
			Body: SubmitAbuseInputBody{
				Slug:   "slug",
				Reason: "unknown_reason",
			},
		}
		_, err := p.submitAbuse(ctx, in)
		if err == nil {
			t.Fatal("expected error for invalid reason")
		}
		if se, ok := err.(huma.StatusError); !ok || se.GetStatus() != http.StatusBadRequest {
			t.Fatalf("got status %v, want 400", err)
		}
	})

	t.Run("valid reasons accepted", func(t *testing.T) {
		for _, r := range []string{"spam", "phishing", "malware", "other", "SPAM", "Phishing"} {
			req := httptest.NewRequest(http.MethodPost, "/abuse", nil)
			req.RemoteAddr = "10.0.0.2:1234"
			in := &SubmitAbuseInput{
				Ctx: mkCtx(req),
				Body: SubmitAbuseInputBody{
					Slug:   "slug",
					Reason: r,
				},
			}
			out, err := p.submitAbuse(ctx, in)
			if err != nil {
				t.Fatalf("reason %q failed: %v", r, err)
			}
			if out.Body.ID == 0 {
				t.Fatalf("reason %q returned empty ID", r)
			}
		}
	})

	t.Run("description truncated to 2000 chars", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/abuse", nil)
		req.RemoteAddr = "10.0.0.3:1234"
		longDesc := strings.Repeat("a", 3000)
		in := &SubmitAbuseInput{
			Ctx: mkCtx(req),
			Body: SubmitAbuseInputBody{
				Slug:        "slug",
				Reason:      "spam",
				Description: longDesc,
			},
		}
		out, err := p.submitAbuse(ctx, in)
		if err != nil {
			t.Fatalf("submitAbuse: %v", err)
		}
		var rep AbuseReport
		p.db.First(&rep, out.Body.ID)
		if len(rep.Description) != 2000 {
			t.Errorf("len(description) = %d, want 2000", len(rep.Description))
		}
	})
}

func TestSubmitAbuseUnresolvedSlugRecordsWithZeroOrgID(t *testing.T) {
	t.Parallel()
	p, mkCtx := setupAbuseTestEnv(t)
	ctx := context.Background()

	req := httptest.NewRequest(http.MethodPost, "/abuse", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	in := &SubmitAbuseInput{
		Ctx: mkCtx(req),
		Body: SubmitAbuseInputBody{
			Slug:        "does-not-exist",
			Reason:      "phishing",
			Description: "Unresolvable slug",
		},
	}
	out, err := p.submitAbuse(ctx, in)
	if err != nil {
		t.Fatalf("submitAbuse on unresolvable slug failed: %v", err)
	}

	var rep AbuseReport
	if err := p.db.First(&rep, out.Body.ID).Error; err != nil {
		t.Fatalf("report not persisted: %v", err)
	}
	if rep.OrgID != 0 {
		t.Errorf("rep.OrgID = %d, want 0", rep.OrgID)
	}
	if rep.Target != "" {
		t.Errorf("rep.Target = %q, want empty", rep.Target)
	}
}

func TestAbuseReportsTenantIsolation(t *testing.T) {
	t.Parallel()
	p, _ := setupAbuseTestEnv(t)
	ctx := context.Background()

	repOrg1 := AbuseReport{OrgID: 1, Slug: "slug-1", Reason: "spam", Status: "open", ReporterIP: "1.2.3.4"}
	repOrg2 := AbuseReport{OrgID: 2, Slug: "slug-2", Reason: "malware", Status: "open", ReporterIP: "5.6.7.8"}
	repOrg0 := AbuseReport{OrgID: 0, Slug: "unresolved", Reason: "phishing", Status: "open", ReporterIP: "9.10.11.12"}
	p.db.Create(&repOrg1)
	p.db.Create(&repOrg2)
	p.db.Create(&repOrg0)

	t.Run("Org 1 admin sees only org 1 reports", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/links/abuse?status=all", nil)
		req.Header.Set("X-Org-ID", "1")
		req.Header.Set("X-Role", "admin")
		in := &ListAbuseReportsInput{
			Ctx:    humago.NewContext(nil, req, httptest.NewRecorder()),
			Status: "all",
		}
		out, err := p.listAbuseReports(ctx, in)
		if err != nil {
			t.Fatalf("listAbuseReports: %v", err)
		}
		if len(out.Body) != 1 || out.Body[0].ID != repOrg1.ID {
			t.Fatalf("got %d reports, want only org 1 report; reports=%+v", len(out.Body), out.Body)
		}
		if out.Body[0].ReporterIP != "1.2.3.*" {
			t.Errorf("reporter ip not masked: %q", out.Body[0].ReporterIP)
		}
	})

	t.Run("Org 1 cannot update Org 2 report", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/links/abuse/2", nil)
		req.Header.Set("X-Org-ID", "1")
		req.Header.Set("X-Role", "admin")
		in := &UpdateAbuseReportInput{
			Ctx:  humago.NewContext(nil, req, httptest.NewRecorder()),
			ID:   repOrg2.ID,
			Body: UpdateAbuseReportInputBody{Status: "reviewed"},
		}
		_, err := p.updateAbuseReport(ctx, in)
		if err == nil {
			t.Fatal("expected 404 for cross-tenant update, got nil")
		}
		if se, ok := err.(huma.StatusError); !ok || se.GetStatus() != http.StatusNotFound {
			t.Fatalf("got status %v, want 404", err)
		}
	})

	t.Run("Org 1 cannot update Org 0 report", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/links/abuse/3", nil)
		req.Header.Set("X-Org-ID", "1")
		req.Header.Set("X-Role", "admin")
		in := &UpdateAbuseReportInput{
			Ctx:  humago.NewContext(nil, req, httptest.NewRecorder()),
			ID:   repOrg0.ID,
			Body: UpdateAbuseReportInputBody{Status: "reviewed"},
		}
		_, err := p.updateAbuseReport(ctx, in)
		if err == nil {
			t.Fatal("expected 404 for org 1 updating org 0 report, got nil")
		}
		if se, ok := err.(huma.StatusError); !ok || se.GetStatus() != http.StatusNotFound {
			t.Fatalf("got status %v, want 404", err)
		}
	})

	t.Run("Instance admin can see org 0 report and their org report", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/links/abuse?status=all", nil)
		req.Header.Set("X-Org-ID", "1")
		req.Header.Set("X-Role", "admin")
		p.isInstanceAdmin = func(r *http.Request) bool { return true }
		defer func() { p.isInstanceAdmin = nil }()

		in := &ListAbuseReportsInput{
			Ctx:    humago.NewContext(nil, req, httptest.NewRecorder()),
			Status: "all",
		}
		out, err := p.listAbuseReports(ctx, in)
		if err != nil {
			t.Fatalf("instance admin list: %v", err)
		}
		var foundZero bool
		for _, r := range out.Body {
			if r.ID == repOrg0.ID {
				foundZero = true
			}
		}
		if !foundZero {
			t.Errorf("instance admin did not see org 0 report; got %+v", out.Body)
		}
	})

	t.Run("Instance admin can update org 0 report", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/links/abuse/3", nil)
		req.Header.Set("X-Org-ID", "1")
		p.isInstanceAdmin = func(r *http.Request) bool { return true }
		defer func() { p.isInstanceAdmin = nil }()

		in := &UpdateAbuseReportInput{
			Ctx:  humago.NewContext(nil, req, httptest.NewRecorder()),
			ID:   repOrg0.ID,
			Body: UpdateAbuseReportInputBody{Status: "reviewed"},
		}
		out, err := p.updateAbuseReport(ctx, in)
		if err != nil {
			t.Fatalf("instance admin update org 0: %v", err)
		}
		if out.Body.Status != "reviewed" {
			t.Errorf("status = %q, want reviewed", out.Body.Status)
		}
	})

	t.Run("Unauthenticated caller rejected with 401", func(t *testing.T) {
		origOrg := p.auth.OrgID
		p.auth.OrgID = func(r *http.Request) uint { return 0 }
		p.auth.UserID = func(r *http.Request) uint { return 0 }
		defer func() { p.auth.OrgID = origOrg }()

		req := httptest.NewRequest(http.MethodGet, "/api/links/abuse", nil)
		in := &ListAbuseReportsInput{
			Ctx: humago.NewContext(nil, req, httptest.NewRecorder()),
		}
		_, err := p.listAbuseReports(ctx, in)
		if err == nil {
			t.Fatal("expected 401 for unauthenticated caller")
		}
		if se, ok := err.(huma.StatusError); !ok || se.GetStatus() != http.StatusUnauthorized {
			t.Fatalf("got status %v, want 401", err)
		}
	})

	t.Run("Non-admin caller rejected with 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/links/abuse", nil)
		req.Header.Set("X-Org-ID", "1")
		req.Header.Set("X-Role", "member")
		in := &ListAbuseReportsInput{
			Ctx: humago.NewContext(nil, req, httptest.NewRecorder()),
		}
		_, err := p.listAbuseReports(ctx, in)
		if err == nil {
			t.Fatal("expected 403 for member caller")
		}
		if se, ok := err.(huma.StatusError); !ok || se.GetStatus() != http.StatusForbidden {
			t.Fatalf("got status %v, want 403", err)
		}
	})
}

func TestAdminUpdateAbuseReport(t *testing.T) {
	t.Parallel()
	p, _ := setupAbuseTestEnv(t)
	ctx := context.Background()

	rep := AbuseReport{OrgID: 1, Slug: "test-update", Reason: "spam", Status: "open", ReporterIP: "203.0.113.100"}
	p.db.Create(&rep)

	t.Run("invalid status returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/links/abuse/1", nil)
		req.Header.Set("X-Org-ID", "1")
		req.Header.Set("X-Role", "admin")
		in := &UpdateAbuseReportInput{
			Ctx:  humago.NewContext(nil, req, httptest.NewRecorder()),
			ID:   rep.ID,
			Body: UpdateAbuseReportInputBody{Status: "invalid_status"},
		}
		_, err := p.updateAbuseReport(ctx, in)
		if err == nil {
			t.Fatal("expected 400 for invalid status")
		}
		if se, ok := err.(huma.StatusError); !ok || se.GetStatus() != http.StatusBadRequest {
			t.Fatalf("got status %v, want 400", err)
		}
	})

	t.Run("update to reviewed succeeds and masks IP", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/links/abuse/1", nil)
		req.Header.Set("X-Org-ID", "1")
		req.Header.Set("X-Role", "admin")
		in := &UpdateAbuseReportInput{
			Ctx:  humago.NewContext(nil, req, httptest.NewRecorder()),
			ID:   rep.ID,
			Body: UpdateAbuseReportInputBody{Status: "reviewed"},
		}
		out, err := p.updateAbuseReport(ctx, in)
		if err != nil {
			t.Fatalf("update to reviewed: %v", err)
		}
		if out.Body.Status != "reviewed" {
			t.Errorf("status = %q, want reviewed", out.Body.Status)
		}
		if out.Body.ReporterIP != "203.0.113.*" {
			t.Errorf("reporter IP = %q, want masked 203.0.113.*", out.Body.ReporterIP)
		}

		var refreshed AbuseReport
		p.db.First(&refreshed, rep.ID)
		if refreshed.Status != "reviewed" {
			t.Errorf("database status = %q, want reviewed", refreshed.Status)
		}
	})

	t.Run("update to dismissed succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/links/abuse/1", nil)
		req.Header.Set("X-Org-ID", "1")
		req.Header.Set("X-Role", "admin")
		in := &UpdateAbuseReportInput{
			Ctx:  humago.NewContext(nil, req, httptest.NewRecorder()),
			ID:   rep.ID,
			Body: UpdateAbuseReportInputBody{Status: "dismissed"},
		}
		out, err := p.updateAbuseReport(ctx, in)
		if err != nil {
			t.Fatalf("update to dismissed: %v", err)
		}
		if out.Body.Status != "dismissed" {
			t.Errorf("status = %q, want dismissed", out.Body.Status)
		}
	})
}

func TestAbusePurge(t *testing.T) {
	t.Parallel()
	p, _ := setupAbuseTestEnv(t)

	p.db.Create(&AbuseReport{OrgID: 1, Slug: "org1-a", Reason: "spam"})
	p.db.Create(&AbuseReport{OrgID: 1, Slug: "org1-b", Reason: "malware"})
	p.db.Create(&AbuseReport{OrgID: 2, Slug: "org2-a", Reason: "phishing"})

	if err := p.purge(1); err != nil {
		t.Fatalf("purge failed: %v", err)
	}

	var count1, count2 int64
	p.db.Model(&AbuseReport{}).Where("owner_id = ?", 1).Count(&count1)
	p.db.Model(&AbuseReport{}).Where("owner_id = ?", 2).Count(&count2)

	if count1 != 0 {
		t.Errorf("org 1 abuse reports after purge: %d, want 0", count1)
	}
	if count2 != 1 {
		t.Errorf("org 2 abuse reports after purge: %d, want 1", count2)
	}
}

func TestAbuseExport(t *testing.T) {
	t.Parallel()
	p, _ := setupAbuseTestEnv(t)

	p.db.Create(&AbuseReport{
		OrgID:      1,
		Slug:       "export-slug",
		Target:     "https://target.example",
		Reason:     "spam",
		ReporterIP: "198.51.100.42",
		Status:     "open",
	})

	exp := p.exportData(1)
	raw, ok := exp["abuseReports"]
	if !ok {
		t.Fatal("exportData does not contain abuseReports")
	}
	reports, ok := raw.([]AbuseReport)
	if !ok {
		t.Fatalf("abuseReports has wrong type: %T", raw)
	}
	if len(reports) != 1 {
		t.Fatalf("exported %d reports, want 1", len(reports))
	}
	if reports[0].ReporterIP == "198.51.100.42" {
		t.Fatal("exportData contains plaintext reporter IP")
	}
	if reports[0].ReporterIP != "198.51.100.*" {
		t.Errorf("exported reporter IP = %q, want 198.51.100.*", reports[0].ReporterIP)
	}
}

func TestAbuseUnscopedPrefixes(t *testing.T) {
	t.Parallel()
	p := New()
	prefixes := p.UnscopedPrefixes()
	if len(prefixes) != 1 || prefixes[0] != "/abuse" {
		t.Fatalf("UnscopedPrefixes = %v, want [/abuse]", prefixes)
	}
	if err := plugin.ValidateUnscopedDeclaration(prefixes[0]); err != nil {
		t.Fatalf("ValidateUnscopedDeclaration failed: %v", err)
	}
}

func TestAbuseMenus(t *testing.T) {
	t.Parallel()
	p := New()
	menus := p.Menus()
	var found bool
	for _, m := range menus {
		if m.ID == "abuse" {
			found = true
			if m.Path != "/links/abuse" {
				t.Errorf("menu path = %q, want /links/abuse", m.Path)
			}
			if m.Category != "Security" {
				t.Errorf("menu category = %q, want Security", m.Category)
			}
			if m.RequiredRole != "admin" {
				t.Errorf("menu requiredRole = %q, want admin", m.RequiredRole)
			}
		}
	}
	if !found {
		t.Fatal("abuse menu item not found in p.Menus()")
	}
}
