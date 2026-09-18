package telegram

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

type mockCrypto struct {
	failEncrypt bool
}

func (c *mockCrypto) Encrypt(plaintext []byte) (string, error) {
	if c.failEncrypt {
		return "", errors.New("simulated encrypt failure")
	}
	return "enc:" + string(plaintext), nil
}

func (c *mockCrypto) Decrypt(encoded string) ([]byte, error) {
	if len(encoded) >= 4 && encoded[:4] == "enc:" {
		return []byte(encoded[4:]), nil
	}
	return nil, errors.New("invalid ciphertext")
}

type mockSettings struct {
	store map[string]string
}

func (s *mockSettings) GetWorkspaceSetting(orgID uint, key string) string {
	return s.store[key]
}

func (s *mockSettings) SetWorkspaceSetting(orgID uint, key, value string) error {
	s.store[key] = value
	return nil
}

func (s *mockSettings) GetGlobalSetting(key string) string       { return "" }
func (s *mockSettings) SetGlobalSetting(key, value string) error { return nil }

type mockEvents struct {
	emailHandler func(plugin.EmailEvent)
}

func (e *mockEvents) PublishEvent(uint, string, any)              {}
func (e *mockEvents) RegisterWebhookEvent(plugin.WebhookEventDef) {}
func (e *mockEvents) OnEmail(handler func(plugin.EmailEvent)) {
	e.emailHandler = handler
}

type mockHost struct {
	session  *mockSession
	crypto   *mockCrypto
	settings *mockSettings
	events   *mockEvents
}

func (h *mockHost) Session() plugin.HostSession {
	if h.session != nil {
		return h.session
	}
	return nil
}
func (h *mockHost) Crypto() plugin.CryptoVault {
	if h.crypto != nil {
		return h.crypto
	}
	return nil
}
func (h *mockHost) Settings() plugin.SettingsStore {
	if h.settings != nil {
		return h.settings
	}
	return nil
}
func (h *mockHost) Events() plugin.EventSpine {
	if h.events != nil {
		return h.events
	}
	return nil
}
func (h *mockHost) TenantDB(orgID uint) *plugin.TenantDB { return nil }

func TestTelegram_Validate(t *testing.T) {
	p := &Plugin{}
	if err := p.Validate(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil host")
	}
	if err := p.Validate(context.Background(), &mockHost{}); err != nil {
		t.Fatalf("unexpected error for valid host: %v", err)
	}
}

func TestTelegram_FailClosedOnZeroOrgID(t *testing.T) {
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
	p.getSettings(rec, httptest.NewRequest("GET", "/api/telegram/settings", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for getSettings with zero orgID, got %d", rec.Code)
	}

	// 2. PUT settings
	rec = httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"chatId":"123"}`))
	p.putSettings(rec, httptest.NewRequest("PUT", "/api/telegram/settings", body))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for putSettings with zero orgID, got %d", rec.Code)
	}

	// 3. POST test
	rec = httptest.NewRecorder()
	p.testSend(rec, httptest.NewRequest("POST", "/api/telegram/test", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for testSend with zero orgID, got %d", rec.Code)
	}
}

func TestTelegram_PutSettingsCryptoErrors(t *testing.T) {
	p := &Plugin{}
	settingsStore := &mockSettings{store: make(map[string]string)}

	// 1. Crypto unavailable when BotToken provided
	hostNoCrypto := &mockHost{
		session: &mockSession{
			orgID: func(r *http.Request) uint { return 1 },
		},
		settings: settingsStore,
	}
	p.host = hostNoCrypto

	rec := httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"botToken":"secret-tok","chatId":"123"}`))
	p.putSettings(rec, httptest.NewRequest("PUT", "/api/telegram/settings", body))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when crypto unavailable and botToken provided, got %d", rec.Code)
	}

	// 2. Encryption failure returns 500
	hostFailCrypto := &mockHost{
		session: &mockSession{
			orgID: func(r *http.Request) uint { return 1 },
		},
		settings: settingsStore,
		crypto:   &mockCrypto{failEncrypt: true},
	}
	p.host = hostFailCrypto

	rec = httptest.NewRecorder()
	body = bytes.NewReader([]byte(`{"botToken":"secret-tok","chatId":"123"}`))
	p.putSettings(rec, httptest.NewRequest("PUT", "/api/telegram/settings", body))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when crypto fails, got %d", rec.Code)
	}

	// 3. Success with valid crypto
	hostOK := &mockHost{
		session: &mockSession{
			orgID: func(r *http.Request) uint { return 1 },
		},
		settings: settingsStore,
		crypto:   &mockCrypto{},
	}
	p.host = hostOK

	rec = httptest.NewRecorder()
	body = bytes.NewReader([]byte(`{"botToken":"secret-tok","chatId":"123"}`))
	p.putSettings(rec, httptest.NewRequest("PUT", "/api/telegram/settings", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on successful putSettings, got %d", rec.Code)
	}

	var out settingsOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !out.HasToken || out.ChatID != "123" {
		t.Fatalf("unexpected settings out: %+v", out)
	}
}
