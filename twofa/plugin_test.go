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
	orgID  func(r *http.Request) uint
	userID func(r *http.Request) uint
}

func (s *mockSession) UserID(r *http.Request) uint                   { return s.userID(r) }
func (s *mockSession) OrgID(r *http.Request) uint                    { return s.orgID(r) }
func (s *mockSession) OrgRole(r *http.Request) string                { return "admin" }
func (s *mockSession) RequireRole(r *http.Request, min string) bool  { return true }
func (s *mockSession) RequirePerm(r *http.Request, p, m string) bool { return true }
func (s *mockSession) IsInstanceAdmin(r *http.Request) bool          { return false }
func (s *mockSession) RevokeUserOrgSessions(u, o uint) int           { return 0 }

type mockHost struct {
	session *mockSession
}

func (h *mockHost) Session() plugin.HostSession          { return h.session }
func (h *mockHost) Crypto() plugin.CryptoVault           { return nil }
func (h *mockHost) Settings() plugin.SettingsStore       { return nil }
func (h *mockHost) Events() plugin.EventSpine            { return nil }
func (h *mockHost) TenantDB(orgID uint) *plugin.TenantDB { return nil }

func setupTestEnv(t *testing.T) (*Plugin, *testMux, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatalf("Failed to open sqlite db: %v", err)
	}

	p := &Plugin{
		key: GetVaultKey("unit-test-vault-key"),
	}

	// Auto-migrate models
	for _, m := range p.Models() {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatalf("AutoMigrate failed: %v", err)
		}
	}

	pCtx := &plugin.Context{
		DB: db,
		Host: &mockHost{
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
