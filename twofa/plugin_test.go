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
	orgID         func(r *http.Request) uint
	userID        func(r *http.Request) uint
	personalOrgID func(r *http.Request) uint
	requireRole   func(r *http.Request, min string) bool
}

func (s *mockSession) UserID(r *http.Request) uint    { return s.userID(r) }
func (s *mockSession) OrgID(r *http.Request) uint     { return s.orgID(r) }
func (s *mockSession) OrgRole(r *http.Request) string { return "admin" }
func (s *mockSession) PersonalOrgID(r *http.Request) uint {
	if s.personalOrgID != nil {
		return s.personalOrgID(r)
	}
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

	// Auto-migrate models
	for _, m := range p.Models() {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatalf("AutoMigrate failed: %v", err)
		}
	}

	reg := plugin.NewRegistry()
	reg.Provide(plugin.ServiceVault, plugin.VaultService(newMockVaultService()))

	pCtx := &plugin.Context{
		DB: db,
		// The app always wraps plugin routes with Guard; mirror that here so
		// Mount registers them. Role checks stay in the handlers under test.
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
				// Each account has its own personal org. User 42 -> 100,
				// user 43 -> 101. X-Test-Personal-Org: 0 models an account
				// that has no personal org.
				personalOrgID: func(r *http.Request) uint {
					if orgHeader := r.Header.Get("X-Test-Personal-Org"); orgHeader != "" {
						var org uint
						_, _ = fmt.Sscanf(orgHeader, "%d", &org)
						return org
					}
					var uid uint
					if userHeader := r.Header.Get("X-Test-User"); userHeader != "" {
						_, _ = fmt.Sscanf(userHeader, "%d", &uid)
					} else {
						uid = 42
					}
					return uid + 58
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

	// 1. Create account (manual)
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

	// 2. List accounts
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

	// 3. Get account with reveal=true
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

	// 4. Verify Code Endpoint
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

	// 5. Toggle Pin
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/pin", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts/{id}/pin", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Toggle pin returned %d: %s", w.Code, w.Body.String())
	}

	// 6. Check Audit Logs
	req = httptest.NewRequest("GET", "/api/twofa/logs", nil)
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/logs", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Get logs returned %d: %s", w.Code, w.Body.String())
	}
	var logs []TwoFAAuditLog
	_ = json.Unmarshal(w.Body.Bytes(), &logs)
	if len(logs) < 2 {
		t.Fatalf("Expected multiple audit logs, got %d", len(logs))
	}

	// 7. Provider contract
	code, rem, err := p.GetCode(context.Background(), 1, "AWS Production")
	if err != nil || len(code) != 6 || rem <= 0 {
		t.Fatalf("Provider.GetCode failed: code=%q, rem=%d, err=%v", code, rem, err)
	}
	valid, err := p.VerifyCode(context.Background(), 1, "AWS Production", code)
	if err != nil || !valid {
		t.Fatalf("Provider.VerifyCode failed: valid=%v, err=%v", valid, err)
	}

	// 8. Health check
	hRes := p.Check(context.Background())
	if hRes.Status != plugin.HealthOK {
		t.Fatalf("Health check returned status %s: %s", hRes.Status, hRes.Message)
	}

	// 9. Multi-tenant isolation test: Org 2 should see 0 accounts
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

	// Import via URI
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

	// Export
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
	// 1. Nil host
	if err := p.Validate(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil host")
	}

	// 2. Host with no VaultService registered
	hNoVault := &mockHost{}
	if err := p.Validate(context.Background(), hNoVault); err == nil {
		t.Fatal("expected error when VaultService is not registered")
	}

	// 3. Mounted with VaultService
	pMounted, _, _ := setupTestEnv(t)
	if err := pMounted.Validate(context.Background(), pMounted.host); err != nil {
		t.Fatalf("expected valid when VaultService is registered: %v", err)
	}
}

func TestPlugin_AdminRoleGated(t *testing.T) {
	p, mux, _ := setupTestEnv(t)

	// Create an account first
	createBody := map[string]any{
		"name":   "Gated Account",
		"secret": "JBSWY3DPEHPK3PXP",
	}
	bodyBytes, _ := json.Marshal(createBody)
	req := httptest.NewRequest("POST", "/api/twofa/accounts", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts", w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Create account failed: %d", w.Code)
	}
	var created AccountSummary
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// Now configure mockSession to reject non-admin (when X-Role == "member")
	mHost := p.host.(*mockHost)
	mHost.session.requireRole = func(r *http.Request, min string) bool {
		return r.Header.Get("X-Role") != "member"
	}

	// 1. Reveal secret with member role -> 403
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/twofa/accounts/%d?reveal=true", created.ID), nil)
	req.Header.Set("X-Role", "member")
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for member reveal secret, got %d", w.Code)
	}

	// 2. Update account with member role -> 403
	upBody, _ := json.Marshal(map[string]any{"name": "Hacked"})
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), bytes.NewReader(upBody))
	req.Header.Set("X-Role", "member")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("PUT /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for member update account, got %d", w.Code)
	}

	// 3. Delete account with member role -> 403
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), nil)
	req.Header.Set("X-Role", "member")
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("DELETE /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for member delete account, got %d", w.Code)
	}

	// 4. Export accounts with member role -> 403
	req = httptest.NewRequest("GET", "/api/twofa/export", nil)
	req.Header.Set("X-Role", "member")
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/export", w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for member export accounts, got %d", w.Code)
	}

	// 5. Get QR with member role -> 403
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/twofa/accounts/%d/qr", created.ID), nil)
	req.Header.Set("X-Role", "member")
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts/{id}/qr", w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for member get QR code, got %d", w.Code)
	}

	// 6. Create account with member role -> 403
	req = httptest.NewRequest("POST", "/api/twofa/accounts", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Role", "member")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts", w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for member create account, got %d", w.Code)
	}

	// 7. Import accounts with member role -> 403
	impBody, _ := json.Marshal(map[string]any{
		"uris": []string{"otpauth://totp/GitHub:octocat?secret=JBSWY3DPEHPK3PXP&issuer=GitHub"},
	})
	req = httptest.NewRequest("POST", "/api/twofa/import", bytes.NewReader(impBody))
	req.Header.Set("X-Role", "member")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.Serve("POST /api/twofa/import", w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for member import accounts, got %d", w.Code)
	}

	// 8. Toggle pin with member role -> 403
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/pin", created.ID), nil)
	req.Header.Set("X-Role", "member")
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("POST /api/twofa/accounts/{id}/pin", w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 for member toggle pin, got %d", w.Code)
	}
}

// TestPlugin_MountWithoutHTTPSurface pins the MCP composition contract: that
// path mounts the plugin with a nil mux and a nil Guard, because it only wants
// the MCP tools. Registering a route there panics the stdio server at startup.
func TestPlugin_MountWithoutHTTPSurface(t *testing.T) {
	p := &Plugin{}
	p.Mount(nil, &plugin.Context{})
}

func TestPlugin_UpdateDeleteCodeQRAndMCP(t *testing.T) {
	p, mux, _ := setupTestEnv(t)

	// Create account
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

	// Update account metadata & tags
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

	// Get live code endpoint
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

	// Get QR endpoint
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/twofa/accounts/%d/qr?format=image", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("GET /api/twofa/accounts/{id}/qr", w, req)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("GetQR failed: code=%d ct=%s", w.Code, w.Header().Get("Content-Type"))
	}

	// MCP list_2fa_accounts
	mcpCtx := plugin.WithOrgID(context.Background(), 1)
	res, _, err := p.mcpListAccounts(mcpCtx, nil, listAccountsInput{Tag: "Edge"})
	if err != nil || res == nil || len(res.Content) == 0 {
		t.Fatalf("mcpListAccounts failed: err=%v res=%+v", err, res)
	}

	// Delete account
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", created.ID))
	w = httptest.NewRecorder()
	mux.Serve("DELETE /api/twofa/accounts/{id}", w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Delete failed: %d %s", w.Code, w.Body.String())
	}

	// Confirm deleted
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

	// Seed a user-scoped twofa_totp item and org-scoped non-twofa items in the same org.
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

			// getAccount (plain and reveal=true)
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

			// updateAccount
			upBody, _ := json.Marshal(map[string]any{"name": "Tampered"})
			req := httptest.NewRequest("PUT", fmt.Sprintf("/api/twofa/accounts/%d", tc.id), bytes.NewReader(upBody))
			req.SetPathValue("id", idStr)
			w := httptest.NewRecorder()
			mux.Serve("PUT /api/twofa/accounts/{id}", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("PUT updateAccount expected 404, got %d (%s)", w.Code, w.Body.String())
			}

			// deleteAccount
			req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/twofa/accounts/%d", tc.id), nil)
			req.SetPathValue("id", idStr)
			w = httptest.NewRecorder()
			mux.Serve("DELETE /api/twofa/accounts/{id}", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("DELETE deleteAccount expected 404, got %d (%s)", w.Code, w.Body.String())
			}

			// togglePin
			req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/pin", tc.id), nil)
			req.SetPathValue("id", idStr)
			w = httptest.NewRecorder()
			mux.Serve("POST /api/twofa/accounts/{id}/pin", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("POST togglePin expected 404, got %d (%s)", w.Code, w.Body.String())
			}

			// getCode
			req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/code", tc.id), nil)
			req.SetPathValue("id", idStr)
			w = httptest.NewRecorder()
			mux.Serve("POST /api/twofa/accounts/{id}/code", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("POST getCode expected 404, got %d (%s)", w.Code, w.Body.String())
			}

			// verifyCode
			verBody, _ := json.Marshal(map[string]string{"code": "123456"})
			req = httptest.NewRequest("POST", fmt.Sprintf("/api/twofa/accounts/%d/verify", tc.id), bytes.NewReader(verBody))
			req.SetPathValue("id", idStr)
			w = httptest.NewRecorder()
			mux.Serve("POST /api/twofa/accounts/{id}/verify", w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("POST verifyCode expected 404, got %d (%s)", w.Code, w.Body.String())
			}

			// getQRCode
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

	// Update TOTP parameters without supplying secret.
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

	// Verify encrypted payload in Vault has synchronized otpauthUri and identical secret.
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

	// Verify ComputeTOTP directly produces an 8-digit code matching the updated algorithm/digits/period.
	code, _, err := vs.ComputeTOTP(context.Background(), 1, created.ID)
	if err != nil {
		t.Fatalf("ComputeTOTP failed: %v", err)
	}
	if len(code) != 8 {
		t.Fatalf("expected 8-digit ComputeTOTP code, got %q", code)
	}
}
