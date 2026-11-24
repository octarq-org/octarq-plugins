package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/llmprovider"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

type mockMailLLMProvider struct {
	name         string
	defaultModel string
	cheapModel   string
	resp         llmprovider.Response
	err          error
	lastReq      llmprovider.Request
}

func (m *mockMailLLMProvider) Name() string         { return m.name }
func (m *mockMailLLMProvider) DefaultModel() string { return m.defaultModel }
func (m *mockMailLLMProvider) CheapModel() string   { return m.cheapModel }
func (m *mockMailLLMProvider) Complete(_ context.Context, req llmprovider.Request) (llmprovider.Response, error) {
	m.lastReq = req
	return m.resp, m.err
}

func setupMailAITestHarness(t *testing.T, llmFn func(uint) (llmprovider.Provider, error)) (*Plugin, *gorm.DB, http.Handler) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	p := New()
	if err := db.AutoMigrate(append(testAllModels(), p.Models()...)...); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("MailTest", "1.0.0"))

	p.Mount(nil, &plugin.Context{
		DB:                db,
		Huma:              api,
		LLMProviderForOrg: llmFn,
		Host: &plugin.TestHost{
			SessionMock: &plugin.TestSession{
				OrgIDFn: func(r *http.Request) uint {
					if val := r.Header.Get("X-Org-ID"); val != "" {
						var id uint
						fmt.Sscanf(val, "%d", &id)
						return id
					}
					return 0
				},
				RequireRoleFn: func(r *http.Request, _ string) bool {
					return r.Header.Get("X-Role") != "member"
				},
			},
			CryptoMock: &plugin.TestCrypto{},
		},
	})
	return p, db, mux
}

func TestMailAIStatusEndpoint(t *testing.T) {
	t.Parallel()

	mockProv := &mockMailLLMProvider{name: "claude", defaultModel: "claude-sonnet", cheapModel: "claude-haiku"}
	_, _, handler := setupMailAITestHarness(t, func(orgID uint) (llmprovider.Provider, error) {
		if orgID == 1 {
			return mockProv, nil
		}
		return nil, errors.New("not configured")
	})

	reqUnauth := httptest.NewRequest(http.MethodGet, "/api/mail/ai/status", nil)
	recUnauth := httptest.NewRecorder()
	handler.ServeHTTP(recUnauth, reqUnauth)
	if recUnauth.Code != http.StatusOK {
		t.Fatalf("unauthenticated status = %d, want 200", recUnauth.Code)
	}
	var unauthBody MailAIStatusOutputBody
	if err := json.Unmarshal(recUnauth.Body.Bytes(), &unauthBody); err != nil {
		t.Fatalf("decode unauth body: %v", err)
	}
	if unauthBody.Configured || unauthBody.Provider != "" {
		t.Fatalf("unauthenticated body = %+v, want {false, \"\"}", unauthBody)
	}

	reqConfigured := httptest.NewRequest(http.MethodGet, "/api/mail/ai/status", nil)
	reqConfigured.Header.Set("X-Org-ID", "1")
	recConfigured := httptest.NewRecorder()
	handler.ServeHTTP(recConfigured, reqConfigured)
	if recConfigured.Code != http.StatusOK {
		t.Fatalf("configured status = %d, want 200", recConfigured.Code)
	}
	var configuredBody MailAIStatusOutputBody
	if err := json.Unmarshal(recConfigured.Body.Bytes(), &configuredBody); err != nil {
		t.Fatalf("decode configured body: %v", err)
	}
	if !configuredBody.Configured || configuredBody.Provider != "claude" {
		t.Fatalf("configured body = %+v, want {true, \"claude\"}", configuredBody)
	}

	reqUnconfigured := httptest.NewRequest(http.MethodGet, "/api/mail/ai/status", nil)
	reqUnconfigured.Header.Set("X-Org-ID", "2")
	recUnconfigured := httptest.NewRecorder()
	handler.ServeHTTP(recUnconfigured, reqUnconfigured)
	if recUnconfigured.Code != http.StatusOK {
		t.Fatalf("unconfigured status = %d, want 200", recUnconfigured.Code)
	}
	var unconfiguredBody MailAIStatusOutputBody
	if err := json.Unmarshal(recUnconfigured.Body.Bytes(), &unconfiguredBody); err != nil {
		t.Fatalf("decode unconfigured body: %v", err)
	}
	if unconfiguredBody.Configured || unconfiguredBody.Provider != "" {
		t.Fatalf("unconfigured body = %+v, want {false, \"\"}", unconfiguredBody)
	}
}

func TestMailAISummarizeEmailGatesAndBehavior(t *testing.T) {
	t.Parallel()

	mockProv := &mockMailLLMProvider{
		name:         "openai",
		defaultModel: "gpt-4o",
		cheapModel:   "gpt-4o-mini",
		resp:         llmprovider.Response{Text: "  Invoice due Friday. Pay $49 by 5 PM.  "},
	}
	_, db, handler := setupMailAITestHarness(t, func(orgID uint) (llmprovider.Provider, error) {
		if orgID == 1 || orgID == 2 {
			return mockProv, nil
		}
		return nil, errors.New("missing provider")
	})

	mb1 := Mailbox{OrgID: 1, Address: "owner@org1.example.com", Enabled: true}
	db.Create(&mb1)
	mb2 := Mailbox{OrgID: 2, Address: "owner@org2.example.com", Enabled: true}
	db.Create(&mb2)

	email1 := Email{
		MailboxID:  mb1.ID,
		FromAddr:   "billing@acme.com",
		ToAddr:     mb1.Address,
		Subject:    "Invoice #101",
		Text:       "Please pay $49 by Friday 5 PM.",
		ReceivedAt: time.Now(),
	}
	db.Create(&email1)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/mail/assist/summarize-email/%d", email1.ID), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("provider not configured returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/mail/assist/summarize-email/%d", email1.ID), nil)
		req.Header.Set("X-Org-ID", "3")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("tenant isolation returns 404 for cross-org email", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/mail/assist/summarize-email/%d", email1.ID), nil)
		req.Header.Set("X-Org-ID", "2")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-existent email returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/mail/assist/summarize-email/999999", nil)
		req.Header.Set("X-Org-ID", "1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("successful summary returns trimmed output", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/mail/assist/summarize-email/%d", email1.ID), nil)
		req.Header.Set("X-Org-ID", "1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
		var out AISummarizeEmailOutputBody
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if out.Summary != "Invoice due Friday. Pay $49 by 5 PM." {
			t.Fatalf("summary = %q", out.Summary)
		}
		if mockProv.lastReq.Model != "gpt-4o-mini" {
			t.Fatalf("model = %q, want gpt-4o-mini", mockProv.lastReq.Model)
		}
		if mockProv.lastReq.MaxTokens != 300 {
			t.Fatalf("maxTokens = %d, want 300", mockProv.lastReq.MaxTokens)
		}
	})
}

func TestMailAISummarizeEmailTruncationAndHTMLFallback(t *testing.T) {
	t.Parallel()

	mockProv := &mockMailLLMProvider{
		name:       "claude",
		cheapModel: "claude-haiku",
		resp:       llmprovider.Response{Text: "Long message summary."},
	}
	_, db, handler := setupMailAITestHarness(t, func(orgID uint) (llmprovider.Provider, error) {
		return mockProv, nil
	})

	mb := Mailbox{OrgID: 1, Address: "inbox@org1.example.com", Enabled: true}
	db.Create(&mb)

	longText := strings.Repeat("中", 9500)
	longEmail := Email{
		MailboxID:  mb.ID,
		FromAddr:   "sender@example.com",
		ToAddr:     mb.Address,
		Subject:    "Long",
		Text:       longText,
		ReceivedAt: time.Now(),
	}
	db.Create(&longEmail)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/mail/assist/summarize-email/%d", longEmail.ID), nil)
	req.Header.Set("X-Org-ID", "1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	prefix := "From: sender@example.com\nSubject: Long\n\n"
	gotContent := mockProv.lastReq.Messages[0].Content
	if !strings.HasPrefix(gotContent, prefix) {
		t.Fatalf("unexpected content prefix")
	}
	gotBody := strings.TrimPrefix(gotContent, prefix)
	if runeLen := utf8.RuneCountInString(gotBody); runeLen != 8000 {
		t.Fatalf("truncated body rune length = %d, want 8000", runeLen)
	}

	htmlEmail := Email{
		MailboxID:  mb.ID,
		FromAddr:   "alerts@example.com",
		ToAddr:     mb.Address,
		Subject:    "HTML Only",
		Text:       "   ",
		HTML:       "<style>.x{color:red}</style><p>Hello <b>World</b></p><script>alert(1)</script>",
		ReceivedAt: time.Now(),
	}
	db.Create(&htmlEmail)

	reqHTML := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/mail/assist/summarize-email/%d", htmlEmail.ID), nil)
	reqHTML.Header.Set("X-Org-ID", "1")
	recHTML := httptest.NewRecorder()
	handler.ServeHTTP(recHTML, reqHTML)
	if recHTML.Code != http.StatusOK {
		t.Fatalf("html status = %d, want 200, body=%s", recHTML.Code, recHTML.Body.String())
	}
	htmlContent := mockProv.lastReq.Messages[0].Content
	if strings.Contains(htmlContent, "<p>") || strings.Contains(htmlContent, "alert(1)") || strings.Contains(htmlContent, "color:red") {
		t.Fatalf("expected HTML tags/style/script stripped, got %q", htmlContent)
	}
	if !strings.Contains(htmlContent, "Hello") || !strings.Contains(htmlContent, "World") {
		t.Fatalf("expected stripped text content, got %q", htmlContent)
	}
}
