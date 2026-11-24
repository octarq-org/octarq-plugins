package mail

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq/server/plugin"
)

func setupMailSettingsTestEnv(t *testing.T) (*Plugin, func(r *http.Request) huma.Context, map[string]string, map[string]string) {
	t.Helper()
	p, mkCtx := setupFullMailTestDB(t)

	wsStore := make(map[string]string)
	gStore := make(map[string]string)

	testSettings := &plugin.TestSettings{
		GetWorkspaceSettingFn: func(orgID uint, key string) string {
			return wsStore[key]
		},
		SetWorkspaceSettingFn: func(orgID uint, key, value string) error {
			wsStore[key] = value
			return nil
		},
		GetGlobalSettingFn: func(key string) string {
			return gStore[key]
		},
		SetGlobalSettingFn: func(key, value string) error {
			gStore[key] = value
			return nil
		},
	}

	p.getWorkspaceSetting = testSettings.GetWorkspaceSetting
	p.setWorkspaceSetting = testSettings.SetWorkspaceSetting
	p.getGlobalSetting = testSettings.GetGlobalSetting
	p.setGlobalSetting = testSettings.SetGlobalSetting

	return p, mkCtx, wsStore, gStore
}

func TestMailSettingsGet(t *testing.T) {
	t.Parallel()
	p, mkCtx, wsStore, gStore := setupMailSettingsTestEnv(t)
	ctx := context.Background()

	wsStore["reserved_mailboxes"] = "billing\nsupport"
	wsStore["catch_all"] = "true"
	wsStore["auto_wrap_links"] = "true"
	gStore["mail_system_sender_id"] = "smtp-1"

	p.db.Create(&testOrg{ID: 1, Slug: "acme-corp", InboundToken: "tok-123"})

	req := httptest.NewRequest(http.MethodGet, "/api/mail/settings", nil)
	in := &GetMailSettingsInput{Ctx: mkCtx(req)}

	out, err := p.getSettings(ctx, in)
	if err != nil {
		t.Fatalf("getSettings error: %v", err)
	}

	if out.Body.ReservedMailboxes != "billing\nsupport" {
		t.Errorf("reservedMailboxes = %q, want billing\\nsupport", out.Body.ReservedMailboxes)
	}
	if !out.Body.CatchAll {
		t.Errorf("catchAll = %v, want true", out.Body.CatchAll)
	}
	if !out.Body.AutoWrapLinks {
		t.Errorf("autoWrapLinks = %v, want true", out.Body.AutoWrapLinks)
	}
	if out.Body.SystemSenderID != "smtp-1" {
		t.Errorf("systemSenderId = %q, want smtp-1", out.Body.SystemSenderID)
	}
	if !out.Body.SystemSenderIDSet {
		t.Errorf("systemSenderIdSet = %v, want true", out.Body.SystemSenderIDSet)
	}
	if out.Body.OrgSlug != "acme-corp" {
		t.Errorf("orgSlug = %q, want acme-corp", out.Body.OrgSlug)
	}
	if out.Body.InboundTokenSet == nil || !*out.Body.InboundTokenSet {
		t.Errorf("inboundTokenSet = %v, want true", out.Body.InboundTokenSet)
	}
}

func TestMailSettingsGetMemberHidesInboundTokenSet(t *testing.T) {
	t.Parallel()
	p, mkCtx, _, _ := setupMailSettingsTestEnv(t)
	ctx := context.Background()

	p.db.Create(&testOrg{ID: 1, Slug: "acme-member", InboundToken: "tok-member"})

	req := httptest.NewRequest(http.MethodGet, "/api/mail/settings", nil)
	req.Header.Set("X-Role", "member")
	in := &GetMailSettingsInput{Ctx: mkCtx(req)}

	out, err := p.getSettings(ctx, in)
	if err != nil {
		t.Fatalf("getSettings error: %v", err)
	}
	if out.Body.InboundTokenSet != nil {
		t.Errorf("member should not see inboundTokenSet, got: %v", *out.Body.InboundTokenSet)
	}
}

func TestMailSettingsGetUnauthenticated(t *testing.T) {
	t.Parallel()
	p, _ := setupFullMailTestDB(t)
	ctx := context.Background()

	req := httptest.NewRequest(http.MethodGet, "/api/mail/settings", nil)
	req.Header.Set("X-Org-ID", "0")
	p.orgID = func(*http.Request) uint { return 0 }

	in := &GetMailSettingsInput{Ctx: humago.NewContext(nil, req, httptest.NewRecorder())}
	_, err := p.getSettings(ctx, in)
	if err == nil {
		t.Fatal("expected 401 error for unauthenticated user")
	}
}

func TestMailSettingsPutAdminGate(t *testing.T) {
	t.Parallel()
	p, mkCtx, _, _ := setupMailSettingsTestEnv(t)
	ctx := context.Background()

	req := httptest.NewRequest(http.MethodPut, "/api/mail/settings", nil)
	req.Header.Set("X-Role", "member")
	newBoxes := "abuse\nsecurity"
	in := &UpdateMailSettingsInput{
		Ctx: mkCtx(req),
		Body: UpdateMailSettingsInputBody{
			ReservedMailboxes: &newBoxes,
		},
	}

	_, err := p.updateSettings(ctx, in)
	if err == nil {
		t.Fatal("expected 403 error for member role")
	}
}

func TestMailSettingsPutSuccessAndPersistence(t *testing.T) {
	t.Parallel()
	p, mkCtx, wsStore, gStore := setupMailSettingsTestEnv(t)
	ctx := context.Background()

	p.db.Create(&testOrg{ID: 1, Slug: "acme-corp", InboundToken: "orig-token"})

	boxes := "admin\nsupport"
	catchAll := true
	autoWrap := true
	sysSender := "smtp-default"
	newToken := "updated-inbound-secret"

	req := httptest.NewRequest(http.MethodPut, "/api/mail/settings", nil)
	in := &UpdateMailSettingsInput{
		Ctx: mkCtx(req),
		Body: UpdateMailSettingsInputBody{
			ReservedMailboxes: &boxes,
			CatchAll:          &catchAll,
			AutoWrapLinks:     &autoWrap,
			SystemSenderID:    &sysSender,
			InboundToken:      &newToken,
		},
	}

	out, err := p.updateSettings(ctx, in)
	if err != nil {
		t.Fatalf("updateSettings failed: %v", err)
	}

	if out.Body.ReservedMailboxes != "admin\nsupport" {
		t.Errorf("reservedMailboxes = %q, want admin\\nsupport", out.Body.ReservedMailboxes)
	}
	if !out.Body.CatchAll {
		t.Errorf("catchAll = %v, want true", out.Body.CatchAll)
	}
	if !out.Body.AutoWrapLinks {
		t.Errorf("autoWrapLinks = %v, want true", out.Body.AutoWrapLinks)
	}
	if out.Body.SystemSenderID != "smtp-default" {
		t.Errorf("systemSenderId = %q, want smtp-default", out.Body.SystemSenderID)
	}

	if wsStore["reserved_mailboxes"] != "admin\nsupport" {
		t.Errorf("wsStore[reserved_mailboxes] = %q", wsStore["reserved_mailboxes"])
	}
	if wsStore["catch_all"] != "true" {
		t.Errorf("wsStore[catch_all] = %q", wsStore["catch_all"])
	}
	if wsStore["auto_wrap_links"] != "true" {
		t.Errorf("wsStore[auto_wrap_links] = %q", wsStore["auto_wrap_links"])
	}
	if gStore["mail_system_sender_id"] != "smtp-default" {
		t.Errorf("gStore[mail_system_sender_id] = %q", gStore["mail_system_sender_id"])
	}

	var updatedOrg testOrg
	if err := p.db.First(&updatedOrg, 1).Error; err != nil {
		t.Fatalf("failed to find org: %v", err)
	}
	if updatedOrg.InboundToken != "updated-inbound-secret" {
		t.Errorf("org.InboundToken = %q, want updated-inbound-secret", updatedOrg.InboundToken)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/mail/settings", nil)
	getOut, err := p.getSettings(ctx, &GetMailSettingsInput{Ctx: mkCtx(getReq)})
	if err != nil {
		t.Fatalf("re-getSettings failed: %v", err)
	}
	if getOut.Body.ReservedMailboxes != "admin\nsupport" || !getOut.Body.CatchAll || !getOut.Body.AutoWrapLinks {
		t.Errorf("persisted settings mismatch: %+v", getOut.Body)
	}
}
