package twofa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type testMux struct {
	handlers map[string]http.Handler
}

func newTestMux() *testMux {
	return &testMux{handlers: make(map[string]http.Handler)}
}

func (m *testMux) Handle(pattern string, handler http.Handler) {
	m.handlers[pattern] = handler
}

func (m *testMux) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	m.handlers[pattern] = http.HandlerFunc(handler)
}

func (m *testMux) Serve(pattern string, w http.ResponseWriter, r *http.Request) {
	if h, ok := m.handlers[pattern]; ok {
		h.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

type mockSession struct {
	orgID       func(r *http.Request) uint
	userID      func(r *http.Request) uint
	requireRole func(r *http.Request, min string) bool
}

func (s *mockSession) UserID(r *http.Request) uint    { return s.userID(r) }
func (s *mockSession) OrgID(r *http.Request) uint     { return s.orgID(r) }
func (s *mockSession) OrgRole(r *http.Request) string { return "admin" }
func (s *mockSession) PersonalOrgID(r *http.Request) uint {
	return 0
}
func (s *mockSession) RequireRole(r *http.Request, min string) bool {
	if s.requireRole != nil {
		return s.requireRole(r, min)
	}
	return true
}
func (s *mockSession) RequirePerm(r *http.Request, p, m string) bool { return true }
func (s *mockSession) IsInstanceAdmin(r *http.Request) bool          { return false }
func (s *mockSession) RevokeUserOrgSessions(u, o uint) int           { return 0 }

type mockHost struct {
	session *mockSession
	db      *gorm.DB
}

func (h *mockHost) Session() plugin.HostSession                                { return h.session }
func (h *mockHost) Crypto() plugin.CryptoVault                                 { return nil }
func (h *mockHost) Settings() plugin.SettingsStore                             { return nil }
func (h *mockHost) Events() plugin.EventSpine                                  { return nil }
func (h *mockHost) HarnessSpec(context.Context, uint) (json.RawMessage, error) { return nil, nil }
func (h *mockHost) Workforce() plugin.Workforce                                { return nil }
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

func setupTestEnv(t *testing.T) (*Plugin, *testMux, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatalf("Failed to open sqlite db: %v", err)
	}

	p := &Plugin{}

	for _, m := range p.Models() {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatalf("AutoMigrate failed: %v", err)
		}
	}

	reg := plugin.NewRegistry()
	reg.Provide(plugin.ServiceVault, plugin.VaultService(newMockVaultService()))

	pCtx := &plugin.Context{
		DB:      db,
		Guard:   func(h http.Handler) http.Handler { return h },
		Provide: reg.Provide,
		Lookup:  reg.Lookup,
		Host: &mockHost{
			db: db,
			session: &mockSession{
				orgID: func(r *http.Request) uint {
					if orgHeader := r.Header.Get("X-Test-Org"); orgHeader != "" {
						var org uint
						_, _ = fmt.Sscanf(orgHeader, "%d", &org)
						return org
					}
					return 1
				},
				userID: func(r *http.Request) uint {
					if userHeader := r.Header.Get("X-Test-User"); userHeader != "" {
						var uid uint
						_, _ = fmt.Sscanf(userHeader, "%d", &uid)
						return uid
					}
					return 42
				},
			},
		},
	}

	mux := newTestMux()
	p.Mount(mux, pCtx)

	return p, mux, db
}

func TestPlugin_FullLifecycle(t *testing.T) {
	p, mux, db := setupTestEnv(t)

	createBody := map[string]any{
		"name":      "AWS Production",
		"issuer":    "Amazon Web Services",
		"account":   "admin@company.com",
		"secret":    "JBSWY3DPEHPK3PXP",
		"tags":      "Cloud, Infra",
		"notes":     "Root login credentials",
		"pinned":    true,
		"algorithm": "SHA1",
		"digits":    6,
		"period":    30,
	}
	bodyBytes, _ := json.Marshal(createBody)
	req := httptest.NewRequest("POST", "/api/twofa/accounts", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts", w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Create account returned status %d: %s", w.Code, w.Body.String())
	}

	var created AccountSummary
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("Failed to parse created response: %v", err)
	}
	if created.ID == 0 || created.Name != "AWS Production" || created.CurrentCode == "" {
		t.Fatalf("Invalid created account: %+v", created)
	}

	req = httptest.NewRequest("GET", "/api/twofa/accounts", nil)
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts", w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("List accounts returned status %d: %s", w.Code, w.Body.String())
	}

	var list []AccountSummary
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("Failed to parse list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("Expected 1 account, got %d", len(list))
	}
	if list[0].CurrentCode == "" {
		t.Fatalf("Expected live TOTP code to be generated, got empty")
	}

	req = httptest.NewRequest("GET", fmt.Sprintf("/api/twofa/accounts/%d?reveal=true", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts/{id}", w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Get account with reveal=true returned %d: %s", w.Code, w.Body.String())
	}
	var revealed map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &revealed)
	if revealed["secret"] != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("Expected revealed secret 'JBSWY3DPEHPK3PXP', got %v", revealed["secret"])
	}

	verifyBody, _ := json.Marshal(map[string]string{"code": created.CurrentCode})
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/verify", created.ID), bytes.NewReader(verifyBody))
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts/{id}/verify", w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Verify code returned %d: %s", w.Code, w.Body.String())
	}
	var vResp map[string]bool
	_ = json.Unmarshal(w.Body.Bytes(), &vResp)
	if !vResp["valid"] {
		t.Fatalf("Expected current live code to be valid, got invalid")
	}

	req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/pin", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts/{id}/pin", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Toggle pin returned %d: %s", w.Code, w.Body.String())
	}

	if w := setShareVia(mux, owner42, created.ID, true); w.Code != http.StatusOK {
		t.Fatalf("Share returned %d: %s", w.Code, w.Body.String())
	}

	code, rem, err := p.GetCode(context.Background(), 1, "AWS Production")
	if err != nil || len(code) != 6 || rem <= 0 {
		t.Fatalf("Provider.GetCode failed: code=%q, rem=%d, err=%v", code, rem, err)
	}
	valid, err := p.VerifyCode(context.Background(), 1, "AWS Production", code)
	if err != nil || !valid {
		t.Fatalf("Provider.VerifyCode failed: valid=%v, err=%v", valid, err)
	}

	hRes := p.Check(context.Background())
	if hRes.Status != plugin.HealthOK {
		t.Fatalf("Health check returned status %s: %s", hRes.Status, hRes.Message)
	}

	reqOrg2 := httptest.NewRequest("GET", "/api/twofa/accounts", nil)
	reqOrg2.Header.Set("X-Test-Org", "2")
	wOrg2 := httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts", wOrg2, reqOrg2)
	var listOrg2 []AccountSummary
	_ = json.Unmarshal(wOrg2.Body.Bytes(), &listOrg2)
	if len(listOrg2) != 0 {
		t.Fatalf("Expected Org 2 to see 0 accounts, got %d", len(listOrg2))
	}

	_ = db
}

func TestPlugin_ImportExport(t *testing.T) {
	_, mux, _ := setupTestEnv(t)

	importBody := map[string]any{
		"uris": []string{
			"otpauth://totp/GitHub:octocat?secret=JBSWY3DPEHPK3PXP&issuer=GitHub",
			"otpauth://totp/Cloudflare:ops@test.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=Cloudflare",
		},
		"tags": "DevOps",
	}
	bodyBytes, _ := json.Marshal(importBody)
	req := httptest.NewRequest("POST", "/api/twofa/import", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()
	mux.Serve("POST /api/twofa/import", w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Import returned status %d: %s", w.Code, w.Body.String())
	}

	var impResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &impResp)
	if impResp["imported"].(float64) != 2 {
		t.Fatalf("Expected 2 imported accounts, got %v", impResp["imported"])
	}

	req = httptest.NewRequest("GET", "/api/twofa/export", nil)
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/export", w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Export returned status %d: %s", w.Code, w.Body.String())
	}
	var exportItems []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &exportItems)
	if len(exportItems) != 2 {
		t.Fatalf("Expected 2 exported items, got %d", len(exportItems))
	}
}

func TestPlugin_ValidatorFailClosed(t *testing.T) {
	p := &Plugin{}
	if err := p.Validate(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil host")
	}

	hNoVault := &mockHost{}
	if err := p.Validate(context.Background(), hNoVault); err == nil {
		t.Fatal("expected error when VaultService is not registered")
	}

	pMounted, _, _ := setupTestEnv(t)
	if err := pMounted.Validate(context.Background(), pMounted.host); err != nil {
		t.Fatalf("expected valid when VaultService is registered: %v", err)
	}
}

func TestPlugin_PersonalVaultNotRoleGated(t *testing.T) {
	p, mux, _ := setupTestEnv(t)

	mHost := p.host.(*mockHost)
	mHost.session.requireRole = func(r *http.Request, min string) bool { return false }

	bodyBytes, _ := json.Marshal(map[string]any{
		"name":   "Member Owned",
		"secret": "JBSWY3DPEHPK3PXP",
	})
	req := httptest.NewRequest("POST", "/api/twofa/accounts", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts", w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("member create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created AccountSummary
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	req = httptest.NewRequest("GET", fmt.Sprintf("/api/twofa/accounts/%d?reveal=true", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("member reveal own account: expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	upBody, _ := json.Marshal(map[string]any{"name": "Renamed"})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), bytes.NewReader(upBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("PUT /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("member update own account: expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/pin", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts/{id}/pin", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("member pin own account: expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("DELETE /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("member delete own account: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestPlugin_MountWithoutHTTPSurface(t *testing.T) {
	p := &Plugin{}
	p.Mount(nil, &plugin.Context{})
}

func TestPlugin_UpdateDeleteCodeQRAndMCP(t *testing.T) {
	p, mux, _ := setupTestEnv(t)

	bodyBytes, _ := json.Marshal(map[string]any{
		"name":    "Cloudflare Prod",
		"issuer":  "Cloudflare",
		"account": "ops@company.com",
		"secret":  "JBSWY3DPEHPK3PXP",
		"tags":    "Cloud",
	})
	req := httptest.NewRequest("POST", "/api/twofa/accounts", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts", w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Create failed: %d %s", w.Code, w.Body.String())
	}
	var created AccountSummary
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	upBytes, _ := json.Marshal(map[string]any{
		"name": "Cloudflare Core",
		"tags": "Cloud, Edge",
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), bytes.NewReader(upBytes))
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("PUT /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Update failed: %d %s", w.Code, w.Body.String())
	}
	var updated AccountSummary
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Name != "Cloudflare Core" || updated.Tags != "Cloud, Edge" {
		t.Fatalf("Unexpected updated account: %+v", updated)
	}

	req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/code", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts/{id}/code", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetCode endpoint failed: %d %s", w.Code, w.Body.String())
	}
	var codeResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &codeResp)
	if c, _ := codeResp["code"].(string); len(c) != 6 {
		t.Fatalf("Expected 6-digit code, got %v", codeResp)
	}

	req = httptest.NewRequest("GET", fmt.Sprintf("/api/twofa/accounts/%d/qr?format=image", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts/{id}/qr", w, req)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("GetQR failed: code=%d ct=%s", w.Code, w.Header().Get("Content-Type"))
	}

	if w := setShareVia(mux, owner42, created.ID, true); w.Code != http.StatusOK {
		t.Fatalf("share failed: %d %s", w.Code, w.Body.String())
	}
	mcpCtx := plugin.WithOrgID(context.Background(), 1)
	res, _, err := p.mcpListAccounts(mcpCtx, nil, listAccountsInput{Tag: "Edge"})
	if err != nil || res == nil || len(res.Content) == 0 {
		t.Fatalf("mcpListAccounts failed: err=%v res=%+v", err, res)
	}

	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("DELETE /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Delete failed: %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404 after delete, got %d", w.Code)
	}
}

func TestPlugin_ItemTypeAndScopeIsolation(t *testing.T) {
	p, mux, _ := setupTestEnv(t)
	vs, err := p.vault()
	if err != nil {
		t.Fatalf("vault unavailable: %v", err)
	}

	userScopedMeta, err := vs.PutSecret(context.Background(), plugin.PutSecretRequest{
		OrgID:          1,
		Scope:          plugin.VaultScopeUser,
		OwnerUserID:    99,
		CollectionSlug: "personal",
		ItemType:       "twofa_totp",
		Name:           "Personal TOTP",
		Metadata:       map[string]any{"issuer": "Personal", "digits": 6, "period": 30, "algorithm": "SHA1"},
		Payload:        map[string]any{"secret": "JBSWY3DPEHPK3PXP", "otpauthUri": "otpauth://totp/Personal:me?secret=JBSWY3DPEHPK3PXP&issuer=Personal"},
	})
	if err != nil {
		t.Fatalf("seed user-scoped item failed: %v", err)
	}

	integrationMeta, err := vs.PutSecret(context.Background(), plugin.PutSecretRequest{
		OrgID:          1,
		Scope:          plugin.VaultScopeOrg,
		CollectionSlug: "integrations",
		ItemType:       "integration",
		ProviderType:   "dns",
		Provider:       "cloudflare",
		Name:           "Cloudflare API Token",
		Payload:        map[string]any{"apiToken": "cf-secret-token"},
	})
	if err != nil {
		t.Fatalf("seed integration item failed: %v", err)
	}

	kvMeta, err := vs.PutSecret(context.Background(), plugin.PutSecretRequest{
		OrgID:          1,
		Scope:          plugin.VaultScopeOrg,
		CollectionSlug: "default",
		ItemType:       "kv_secret",
		Name:           "Database Password",
		Payload:        map[string]any{"password": "super-secret-password"},
	})
	if err != nil {
		t.Fatalf("seed kv_secret item failed: %v", err)
	}

	targets := []struct {
		label string
		id    uint
	}{
		{"user_scoped_twofa", userScopedMeta.ID},
		{"org_integration", integrationMeta.ID},
		{"org_kv_secret", kvMeta.ID},
	}

	for _, tc := range targets {
		t.Run(tc.label, func(t *testing.T) {
			idStr := fmt.Sprintf("%d", tc.id)

			for _, targetURL := range []string{
				fmt.Sprintf("/api/twofa/accounts/%d", tc.id),
				fmt.Sprintf("/api/twofa/accounts/%d?reveal=true", tc.id),
			} {
				req := httptest.NewRequest("GET", targetURL, nil)
				req.SetPathValue("id", idStr)
				w := httptest.NewRecorder()
				mux.Serve("GET /api/twofa/accounts/{id}", w, req)
				if w.Code != http.StatusNotFound {
					t.Fatalf("GET %s expected 404, got %d (%s)", targetURL, w.Code, w.Body.String())
				}
			}

			upBody, _ := json.Marshal(map[string]any{"name": "Tampered"})
			req := httptest.NewRequest("PUT", fmt.Sprintf("/api/twofa/accounts/%d", tc.id), bytes.NewReader(upBody))
			req.SetPathValue("id", idStr)
			w := httptest.NewRecorder()
			mux.Serve("PUT /api/twofa/accounts/{id}", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("PUT updateAccount expected 404, got %d (%s)", w.Code, w.Body.String())
			}

			req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/twofa/accounts/%d", tc.id), nil)
			req.SetPathValue("id", idStr)
			w = httptest.NewRecorder()
			mux.Serve("DELETE /api/twofa/accounts/{id}", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("DELETE deleteAccount expected 404, got %d (%s)", w.Code, w.Body.String())
			}

			req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/pin", tc.id), nil)
			req.SetPathValue("id", idStr)
			w = httptest.NewRecorder()
			mux.Serve("POST /api/twofa/accounts/{id}/pin", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("POST togglePin expected 404, got %d (%s)", w.Code, w.Body.String())
			}

			req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/code", tc.id), nil)
			req.SetPathValue("id", idStr)
			w = httptest.NewRecorder()
			mux.Serve("POST /api/twofa/accounts/{id}/code", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("POST getCode expected 404, got %d (%s)", w.Code, w.Body.String())
			}

			verBody, _ := json.Marshal(map[string]string{"code": "123456"})
			req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/verify", tc.id), bytes.NewReader(verBody))
			req.SetPathValue("id", idStr)
			w = httptest.NewRecorder()
			mux.Serve("POST /api/twofa/accounts/{id}/verify", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("POST verifyCode expected 404, got %d (%s)", w.Code, w.Body.String())
			}

			req = httptest.NewRequest("GET", fmt.Sprintf("/api/twofa/accounts/%d/qr", tc.id), nil)
			req.SetPathValue("id", idStr)
			w = httptest.NewRecorder()
			mux.Serve("GET /api/twofa/accounts/{id}/qr", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("GET getQRCode expected 404, got %d (%s)", w.Code, w.Body.String())
			}
		})
	}
}

func TestPlugin_UpdateTOTPParamsSyncsOTPAuthURI(t *testing.T) {
	p, mux, _ := setupTestEnv(t)
	vs, err := p.vault()
	if err != nil {
		t.Fatalf("vault unavailable: %v", err)
	}

	const initialSecret = "JBSWY3DPEHPK3PXP"
	createBody, _ := json.Marshal(map[string]any{
		"name":      "GitHub Prod",
		"issuer":    "GitHub",
		"account":   "ops@company.com",
		"secret":    initialSecret,
		"algorithm": "SHA1",
		"digits":    6,
		"period":    30,
	})
	req := httptest.NewRequest("POST", "/api/twofa/accounts", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts", w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", w.Code, w.Body.String())
	}
	var created AccountSummary
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	upBody, _ := json.Marshal(map[string]any{
		"issuer":    "GitHub Enterprise",
		"account":   "secops@company.com",
		"algorithm": "SHA256",
		"digits":    8,
		"period":    60,
	})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), bytes.NewReader(upBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("PUT /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update failed: %d %s", w.Code, w.Body.String())
	}

	var updated AccountSummary
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated response failed: %v", err)
	}
	if len(updated.CurrentCode) != 8 {
		t.Fatalf("expected 8-digit code from ComputeTOTP after update, got %q", updated.CurrentCode)
	}

	sec, err := vs.GetSecret(context.Background(), 1, created.ID)
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if got := sec.Payload["secret"]; got != initialSecret {
		t.Fatalf("expected secret %q to remain unchanged, got %v", initialSecret, got)
	}
	uri, _ := sec.Payload["otpauthUri"].(string)
	parsedAcc, parsedSecret, err := ParseOTPAuthURI(uri)
	if err != nil {
		t.Fatalf("ParseOTPAuthURI(%q) failed: %v", uri, err)
	}
	if parsedSecret != initialSecret {
		t.Fatalf("expected otpauthUri secret %q, got %q", initialSecret, parsedSecret)
	}
	if parsedAcc.Issuer != "GitHub Enterprise" || parsedAcc.Account != "secops@company.com" ||
		parsedAcc.Algorithm != "SHA256" || parsedAcc.Digits != 8 || parsedAcc.Period != 60 {
		t.Fatalf("otpauthUri params out of sync: %+v (uri=%q)", parsedAcc, uri)
	}

	code, _, err := vs.ComputeTOTP(context.Background(), 1, created.ID)
	if err != nil {
		t.Fatalf("ComputeTOTP failed: %v", err)
	}
	if len(code) != 8 {
		t.Fatalf("expected 8-digit ComputeTOTP code, got %q", code)
	}
}

func TestPlugin_AuditReceivesNonNilRequestHTTPAndMCP(t *testing.T) {
	p, mux, _ := setupTestEnv(t)

	type auditCall struct {
		req        *http.Request
		action     string
		targetType string
		targetID   uint
		meta       map[string]any
	}
	var calls []auditCall
	p.ctx.Audit = func(r *http.Request, action, targetType string, targetID uint, meta map[string]any) {
		if r == nil {
			t.Fatalf("p.ctx.Audit called with nil *http.Request for action %q", action)
		}
		calls = append(calls, auditCall{
			req:        r,
			action:     action,
			targetType: targetType,
			targetID:   targetID,
			meta:       meta,
		})
	}

	p.logAudit(nil, 1, 0, "ignored", "noop", "actor", "127.0.0.1")
	if len(calls) != 0 {
		t.Fatalf("expected nil request logAudit to be a no-op, got %d calls", len(calls))
	}

	createBody, _ := json.Marshal(map[string]any{
		"name":   "Audit HTTP Test",
		"secret": "JBSWY3DPEHPK3PXP",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/twofa/accounts", bytes.NewReader(createBody))
	req.RemoteAddr = "198.51.100.10:1234"
	w := httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts", w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", w.Code, w.Body.String())
	}
	var created AccountSummary
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	reqReveal := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/twofa/accounts/%d?reveal=true", created.ID), nil)
	reqReveal.RemoteAddr = "198.51.100.11:5678"
	reqReveal.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts/{id}", w, reqReveal)
	if w.Code != http.StatusOK {
		t.Fatalf("reveal failed: %d %s", w.Code, w.Body.String())
	}

	reqCode := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/twofa/accounts/%d/code", created.ID), nil)
	reqCode.RemoteAddr = "198.51.100.12:9999"
	reqCode.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts/{id}/code", w, reqCode)
	if w.Code != http.StatusOK {
		t.Fatalf("code failed: %d %s", w.Code, w.Body.String())
	}

	if w := setShareVia(mux, owner42, created.ID, true); w.Code != http.StatusOK {
		t.Fatalf("share failed: %d %s", w.Code, w.Body.String())
	}

	mcpCtx := plugin.WithOrgID(context.Background(), 1)
	if _, _, err := p.mcpGetCode(mcpCtx, nil, getCodeInput{AccountName: "Audit HTTP Test"}); err != nil {
		t.Fatalf("mcpGetCode failed: %v", err)
	}
	if _, _, err := p.mcpVerifyCode(mcpCtx, nil, verifyCodeInput{AccountName: "Audit HTTP Test", Code: created.CurrentCode}); err != nil {
		t.Fatalf("mcpVerifyCode failed: %v", err)
	}

	if len(calls) != 6 {
		t.Fatalf("expected 6 audit calls, got %d: %+v", len(calls), calls)
	}
	if calls[0].action != "twofa.create_account" || calls[0].req != req {
		t.Fatalf("expected call 0 to forward original HTTP request, got action=%s req=%p", calls[0].action, calls[0].req)
	}
	if calls[1].action != "twofa.view_secret" || calls[1].req != reqReveal {
		t.Fatalf("expected call 1 to forward reveal HTTP request, got action=%s req=%p", calls[1].action, calls[1].req)
	}
	if calls[2].action != "twofa.generate_code" || calls[2].req != reqCode {
		t.Fatalf("expected call 2 to forward code HTTP request, got action=%s req=%p", calls[2].action, calls[2].req)
	}
	if calls[3].action != "twofa.share_account" {
		t.Fatalf("expected call 3 to be share_account, got %q", calls[3].action)
	}
	for _, idx := range []int{4, 5} {
		c := calls[idx]
		if c.req.RemoteAddr != "mcp" {
			t.Fatalf("MCP audit call %d expected RemoteAddr=\"mcp\", got %q", idx, c.req.RemoteAddr)
		}
		if gotOrg := plugin.OrgIDFromContext(c.req.Context()); gotOrg != 1 {
			t.Fatalf("MCP audit call %d expected orgID=1 in request context, got %d", idx, gotOrg)
		}
	}
	if calls[4].action != "twofa.mcp_get_code" || calls[5].action != "twofa.mcp_verify_code" {
		t.Fatalf("unexpected MCP audit actions: %q, %q", calls[4].action, calls[5].action)
	}
}

func TestPlugin_UpdateAccountClearsOptionalFields(t *testing.T) {
	p, mux, _ := setupTestEnv(t)
	vs, err := p.vault()
	if err != nil {
		t.Fatalf("vault unavailable: %v", err)
	}

	createBody, _ := json.Marshal(map[string]any{
		"name":    "AWS Prod",
		"issuer":  "Amazon",
		"account": "root@acme.com",
		"secret":  "JBSWY3DPEHPK3PXP",
		"tags":    "Cloud, Prod",
		"notes":   "Backup key",
		"pinned":  true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/twofa/accounts", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts", w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", w.Code, w.Body.String())
	}
	var created AccountSummary
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	partialBody, _ := json.Marshal(map[string]any{
		"tags": "Cloud",
	})
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/twofa/accounts/%d", created.ID), bytes.NewReader(partialBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("PUT /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("partial update failed: %d %s", w.Code, w.Body.String())
	}
	var partial AccountSummary
	_ = json.Unmarshal(w.Body.Bytes(), &partial)
	if partial.Name != "AWS Prod" || partial.Issuer != "Amazon" || partial.Account != "root@acme.com" ||
		partial.Notes != "Backup key" || partial.Algorithm != "SHA1" || !partial.Pinned || partial.Tags != "Cloud" {
		t.Fatalf("partial update did not preserve omitted fields: %+v", partial)
	}

	clearBody, _ := json.Marshal(map[string]any{
		"issuer":    "",
		"account":   "",
		"tags":      "",
		"notes":     "",
		"name":      "",
		"algorithm": "",
	})
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/twofa/accounts/%d", created.ID), bytes.NewReader(clearBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("PUT /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear update failed: %d %s", w.Code, w.Body.String())
	}
	var cleared AccountSummary
	_ = json.Unmarshal(w.Body.Bytes(), &cleared)
	if cleared.Name != "AWS Prod" || cleared.Algorithm != "SHA1" {
		t.Fatalf("empty name/algorithm should preserve existing required values, got %+v", cleared)
	}
	if cleared.Issuer != "" || cleared.Account != "" || cleared.Tags != "" || cleared.Notes != "" {
		t.Fatalf("expected issuer, account, tags, notes to be cleared, got %+v", cleared)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/twofa/accounts", nil)
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list failed: %d %s", w.Code, w.Body.String())
	}
	var list []AccountSummary
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Issuer != "" || list[0].Account != "" || list[0].Tags != "" || list[0].Notes != "" {
		t.Fatalf("expected cleared fields to stay empty on GET list, got %+v", list)
	}

	sec, err := vs.GetSecret(context.Background(), 1, created.ID)
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	uri, _ := sec.Payload["otpauthUri"].(string)
	parsedAcc, _, err := ParseOTPAuthURI(uri)
	if err != nil {
		t.Fatalf("ParseOTPAuthURI(%q) failed: %v", uri, err)
	}
	if parsedAcc.Issuer != "" || parsedAcc.Account != "AWS Prod" {
		t.Fatalf("expected otpauthUri to drop cleared issuer/account and use account name label, got %+v (uri=%q)", parsedAcc, uri)
	}
}
