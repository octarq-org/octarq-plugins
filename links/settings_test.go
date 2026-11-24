package links

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq/server/plugin"
)

func setupLinksSettingsTestEnv(t *testing.T) (*Plugin, func(r *http.Request) huma.Context, map[string]string) {
	t.Helper()
	p, mkCtx := setupFullLinksTestDB(t)

	gStore := make(map[string]string)
	testSettings := &plugin.TestSettings{
		GetGlobalSettingFn: func(key string) string {
			return gStore[key]
		},
		SetGlobalSettingFn: func(key, value string) error {
			gStore[key] = value
			return nil
		},
	}

	p.getGlobalSetting = testSettings.GetGlobalSetting
	p.setGlobalSetting = testSettings.SetGlobalSetting

	return p, mkCtx, gStore
}

func TestLinksSettingsGet(t *testing.T) {
	t.Parallel()
	p, mkCtx, gStore := setupLinksSettingsTestEnv(t)
	ctx := context.Background()

	gStore["reserved_slugs"] = "preview\ninternal\nstatus"

	req := httptest.NewRequest(http.MethodGet, "/api/links/settings", nil)
	in := &GetLinksSettingsInput{Ctx: mkCtx(req)}

	out, err := p.getSettings(ctx, in)
	if err != nil {
		t.Fatalf("getSettings error: %v", err)
	}
	if out.Body.ReservedSlugs != "preview\ninternal\nstatus" {
		t.Errorf("reservedSlugs = %q, want preview\\ninternal\\nstatus", out.Body.ReservedSlugs)
	}
}

func TestLinksSettingsGetUnauthenticated(t *testing.T) {
	t.Parallel()
	p, _ := setupFullLinksTestDB(t)
	ctx := context.Background()

	req := httptest.NewRequest(http.MethodGet, "/api/links/settings", nil)
	p.auth.OrgID = func(*http.Request) uint { return 0 }

	in := &GetLinksSettingsInput{Ctx: humago.NewContext(nil, req, httptest.NewRecorder())}
	_, err := p.getSettings(ctx, in)
	if err == nil {
		t.Fatal("expected 401 error for unauthenticated user")
	}
}

func TestLinksSettingsPutInstanceAdminGate(t *testing.T) {
	t.Parallel()
	p, mkCtx, _ := setupLinksSettingsTestEnv(t)
	ctx := context.Background()

	p.isInstanceAdmin = func(*http.Request) bool { return false }

	req := httptest.NewRequest(http.MethodPut, "/api/links/settings", nil)
	in := &UpdateLinksSettingsInput{
		Ctx: mkCtx(req),
		Body: UpdateLinksSettingsInputBody{
			ReservedSlugs: "test\nslug",
		},
	}

	_, err := p.updateSettings(ctx, in)
	if err == nil {
		t.Fatal("expected 403 error for non-instance-admin")
	}
}

func TestLinksSettingsPutSuccessAndRejectionOnLinkCreate(t *testing.T) {
	t.Parallel()
	p, mkCtx, gStore := setupLinksSettingsTestEnv(t)
	ctx := context.Background()

	p.isInstanceAdmin = func(*http.Request) bool { return true }

	req := httptest.NewRequest(http.MethodPut, "/api/links/settings", nil)
	in := &UpdateLinksSettingsInput{
		Ctx: mkCtx(req),
		Body: UpdateLinksSettingsInputBody{
			ReservedSlugs: "supersecret\nconfidential",
		},
	}

	out, err := p.updateSettings(ctx, in)
	if err != nil {
		t.Fatalf("updateSettings error: %v", err)
	}

	if out.Body.ReservedSlugs != "supersecret\nconfidential" {
		t.Errorf("reservedSlugs = %q, want supersecret\\nconfidential", out.Body.ReservedSlugs)
	}
	if gStore["reserved_slugs"] != "supersecret\nconfidential" {
		t.Errorf("gStore[reserved_slugs] = %q", gStore["reserved_slugs"])
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/links", nil)
	createIn := &CreateLinkInput{
		Ctx: mkCtx(createReq),
		Body: linkDTO{
			Slug:   "supersecret",
			Target: "https://example.com/forbidden",
		},
	}
	_, createErr := p.createLink(ctx, createIn)
	if createErr == nil {
		t.Fatal("expected error creating link with reserved slug")
	}
	humaErr, ok := createErr.(huma.StatusError)
	if !ok || humaErr.GetStatus() != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got: %v", createErr)
	}
}
