package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/octarq-org/octarq/server/plugin"
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

type mockSettings struct {
	store map[string]string
}

func (s *mockSettings) GetWorkspaceSetting(orgID uint, key string) string {
	return s.store[key]
}

func (s *mockSettings) SetWorkspaceSetting(orgID uint, key, value string) error {
	if s.store == nil {
		return errors.New("nil store")
	}
	s.store[key] = value
	return nil
}

func (s *mockSettings) GetGlobalSetting(key string) string       { return "" }
func (s *mockSettings) SetGlobalSetting(key, value string) error { return nil }

type mockHost struct {
	session  *mockSession
	settings *mockSettings
}

func (h *mockHost) Session() plugin.HostSession          { return h.session }
func (h *mockHost) Crypto() plugin.CryptoVault           { return nil }
func (h *mockHost) Settings() plugin.SettingsStore       { return h.settings }
func (h *mockHost) Events() plugin.EventSpine            { return nil }
func (h *mockHost) TenantDB(orgID uint) *plugin.TenantDB { return nil }

func TestWebhook_Validate(t *testing.T) {
	p := &Plugin{}
	if err := p.Validate(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil host")
	}
	if err := p.Validate(context.Background(), &mockHost{}); err != nil {
		t.Fatalf("unexpected error for valid host: %v", err)
	}
}

func TestWebhook_FailClosedOnZeroOrgID(t *testing.T) {
	p := &Plugin{}
	host := &mockHost{
		session: &mockSession{
			orgID: func(r *http.Request) uint { return 0 },
		},
		settings: &mockSettings{store: make(map[string]string)},
	}
	p.host = host

	// 1. GET settings
	rec := httptest.NewRecorder()
	p.getSettings(rec, httptest.NewRequest("GET", "/api/webhook/settings", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for getSettings with zero orgID, got %d", rec.Code)
	}

	// 2. PUT settings
	rec = httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"url":"https://example.com/webhook"}`))
	p.putSettings(rec, httptest.NewRequest("PUT", "/api/webhook/settings", body))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for putSettings with zero orgID, got %d", rec.Code)
	}

	// 3. POST test
	rec = httptest.NewRecorder()
	p.testSend(rec, httptest.NewRequest("POST", "/api/webhook/test", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for testSend with zero orgID, got %d", rec.Code)
	}
}

func TestWebhook_PutSettingsSuccess(t *testing.T) {
	p := &Plugin{}
	settingsStore := &mockSettings{store: make(map[string]string)}
	host := &mockHost{
		session: &mockSession{
			orgID: func(r *http.Request) uint { return 1 },
		},
		settings: settingsStore,
	}
	p.host = host

	rec := httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"url":"https://example.com/hook"}`))
	p.putSettings(rec, httptest.NewRequest("PUT", "/api/webhook/settings", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var out settingsOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if out.URL != "https://example.com/hook" {
		t.Fatalf("expected url saved, got %s", out.URL)
	}
}
