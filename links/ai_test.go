package links

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq/server/llmprovider"
	"github.com/octarq-org/octarq/server/plugin"
)

type stubLLMProvider struct {
	name       string
	cheapModel string
	completeFn func(ctx context.Context, req llmprovider.Request) (llmprovider.Response, error)
}

func (s *stubLLMProvider) Name() string         { return s.name }
func (s *stubLLMProvider) DefaultModel() string { return s.cheapModel }
func (s *stubLLMProvider) CheapModel() string   { return s.cheapModel }
func (s *stubLLMProvider) Complete(ctx context.Context, req llmprovider.Request) (llmprovider.Response, error) {
	return s.completeFn(ctx, req)
}

func setupLinksAIHTTP(t *testing.T, resolver func(orgID uint) (llmprovider.Provider, error)) (*Plugin, *http.ServeMux) {
	t.Helper()
	seed, _ := setupFullLinksTestDB(t)
	wipeLinksTables(t, seed)

	p := New()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("links ai test", "1.0.0"))
	pctx := &plugin.Context{
		DB: seed.db,
		Host: &plugin.TestHost{
			SessionMock: &plugin.TestSession{
				UserIDFn: func(*http.Request) uint { return 1 },
				OrgIDFn: func(r *http.Request) uint {
					if v := r.Header.Get("X-Org-ID"); v != "" {
						var id uint
						fmt.Sscanf(v, "%d", &id)
						return id
					}
					return 0
				},
				RequireRoleFn: func(*http.Request, string) bool { return true },
			},
			EventsMock:   &plugin.TestEvents{},
			SettingsMock: &plugin.TestSettings{},
		},
		Huma:              api,
		LLMProviderForOrg: resolver,
	}
	p.Mount(nil, pctx)
	return p, mux
}

func doJSONRequest(mux *http.ServeMux, method, path, orgID string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestLinksAIStatus(t *testing.T) {
	t.Parallel()

	_, muxUnconfigured := setupLinksAIHTTP(t, nil)
	rec := doJSONRequest(muxUnconfigured, http.MethodGet, "/api/links/ai/status", "1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/links/ai/status unconfigured -> %d, want 200", rec.Code)
	}
	var status LinkAIStatusOutputBody
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Configured || status.Provider != "" {
		t.Fatalf("got %+v, want {Configured:false Provider:\"\"}", status)
	}

	_, muxConfigured := setupLinksAIHTTP(t, func(orgID uint) (llmprovider.Provider, error) {
		if orgID == 1 {
			return &stubLLMProvider{name: "openai"}, nil
		}
		return nil, errors.New("not configured")
	})

	recUnauth := doJSONRequest(muxConfigured, http.MethodGet, "/api/links/ai/status", "", nil)
	if recUnauth.Code != http.StatusOK {
		t.Fatalf("GET /api/links/ai/status unauth -> %d, want 200", recUnauth.Code)
	}
	var unauthStatus LinkAIStatusOutputBody
	if err := json.Unmarshal(recUnauth.Body.Bytes(), &unauthStatus); err != nil {
		t.Fatalf("decode unauth status: %v", err)
	}
	if unauthStatus.Configured || unauthStatus.Provider != "" {
		t.Fatalf("unauth got %+v, want {Configured:false Provider:\"\"}", unauthStatus)
	}

	recAuth := doJSONRequest(muxConfigured, http.MethodGet, "/api/links/ai/status", "1", nil)
	if recAuth.Code != http.StatusOK {
		t.Fatalf("GET /api/links/ai/status auth -> %d, want 200", recAuth.Code)
	}
	var authStatus LinkAIStatusOutputBody
	if err := json.Unmarshal(recAuth.Body.Bytes(), &authStatus); err != nil {
		t.Fatalf("decode auth status: %v", err)
	}
	if !authStatus.Configured || authStatus.Provider != "openai" {
		t.Fatalf("auth got %+v, want {Configured:true Provider:\"openai\"}", authStatus)
	}
}

func TestLinksAISuggestSlugValidationAndErrors(t *testing.T) {
	t.Parallel()

	_, muxNilResolver := setupLinksAIHTTP(t, nil)

	recUnauth := doJSONRequest(muxNilResolver, http.MethodPost, "/api/links/ai/suggest-slug", "", map[string]string{
		"target": "https://example.com/post",
	})
	if recUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated suggest -> %d, want 401 (body: %s)", recUnauth.Code, recUnauth.Body.String())
	}

	recEmptyTarget := doJSONRequest(muxNilResolver, http.MethodPost, "/api/links/ai/suggest-slug", "1", map[string]string{
		"target": "   ",
	})
	if recEmptyTarget.Code != http.StatusBadRequest {
		t.Fatalf("empty target suggest -> %d, want 400 (body: %s)", recEmptyTarget.Code, recEmptyTarget.Body.String())
	}

	recNotConfigured := doJSONRequest(muxNilResolver, http.MethodPost, "/api/links/ai/suggest-slug", "1", map[string]string{
		"target": "https://example.com/post",
	})
	if recNotConfigured.Code != http.StatusBadRequest {
		t.Fatalf("nil resolver suggest -> %d, want 400 (body: %s)", recNotConfigured.Code, recNotConfigured.Body.String())
	}

	_, muxErrResolver := setupLinksAIHTTP(t, func(uint) (llmprovider.Provider, error) {
		return nil, errors.New("missing key")
	})
	recErrResolver := doJSONRequest(muxErrResolver, http.MethodPost, "/api/links/ai/suggest-slug", "1", map[string]string{
		"target": "https://example.com/post",
	})
	if recErrResolver.Code != http.StatusBadRequest {
		t.Fatalf("error resolver suggest -> %d, want 400 (body: %s)", recErrResolver.Code, recErrResolver.Body.String())
	}

	_, muxCompleteErr := setupLinksAIHTTP(t, func(uint) (llmprovider.Provider, error) {
		return &stubLLMProvider{
			name:       "openai",
			cheapModel: "gpt-4o-mini",
			completeFn: func(context.Context, llmprovider.Request) (llmprovider.Response, error) {
				return llmprovider.Response{}, errors.New("upstream rate limit")
			},
		}, nil
	})
	recCompleteErr := doJSONRequest(muxCompleteErr, http.MethodPost, "/api/links/ai/suggest-slug", "1", map[string]string{
		"target": "https://example.com/post",
	})
	if recCompleteErr.Code != http.StatusBadGateway {
		t.Fatalf("complete error suggest -> %d, want 502 (body: %s)", recCompleteErr.Code, recCompleteErr.Body.String())
	}
}

func TestLinksAISuggestSlugSuccessAndFiltering(t *testing.T) {
	t.Parallel()

	var capturedReq llmprovider.Request
	_, mux := setupLinksAIHTTP(t, func(uint) (llmprovider.Provider, error) {
		return &stubLLMProvider{
			name:       "claude",
			cheapModel: "claude-haiku",
			completeFn: func(_ context.Context, req llmprovider.Request) (llmprovider.Response, error) {
				capturedReq = req
				return llmprovider.Response{
					Text: "```json\n[\"ab\", \"Valid-Slug\", \"has_underscore\", \"launch-2026\", \"too-long-slug-exceeding-thirty-chars-limit\", \"octarq-docs\", \"bad/slash\", \"quick-link\", \"fifth-slug\", \"sixth-slug\"]\n```",
				}, nil
			},
		}, nil
	})

	rec := doJSONRequest(mux, http.MethodPost, "/api/links/ai/suggest-slug", "1", map[string]string{
		"target": "https://example.com/launch",
		"title":  "Product Launch 2026",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("suggest -> %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var out LinkAISuggestSlugOutputBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode suggest: %v", err)
	}
	want := []string{"valid-slug", "launch-2026", "octarq-docs", "quick-link", "fifth-slug"}
	if len(out.Slugs) != len(want) {
		t.Fatalf("slugs = %v, want %v", out.Slugs, want)
	}
	for i := range want {
		if out.Slugs[i] != want[i] {
			t.Errorf("slugs[%d] = %q, want %q", i, out.Slugs[i], want[i])
		}
	}
	if capturedReq.Model != "claude-haiku" || !capturedReq.JSON || capturedReq.MaxTokens != 200 {
		t.Errorf("unexpected llm request config: %+v", capturedReq)
	}
	if len(capturedReq.Messages) != 1 || !strings.Contains(capturedReq.Messages[0].Content, "https://example.com/launch") || !strings.Contains(capturedReq.Messages[0].Content, "Product Launch 2026") {
		t.Errorf("unexpected prompt messages: %+v", capturedReq.Messages)
	}

	_, muxNoValid := setupLinksAIHTTP(t, func(uint) (llmprovider.Provider, error) {
		return &stubLLMProvider{
			name:       "claude",
			cheapModel: "claude-haiku",
			completeFn: func(context.Context, llmprovider.Request) (llmprovider.Response, error) {
				return llmprovider.Response{Text: `["ab", "invalid_slug", "way-too-long-slug-exceeding-thirty-characters"]`}, nil
			},
		}, nil
	})
	recNoValid := doJSONRequest(muxNoValid, http.MethodPost, "/api/links/ai/suggest-slug", "1", map[string]string{
		"target": "https://example.com/launch",
	})
	if recNoValid.Code != http.StatusBadGateway {
		t.Fatalf("all invalid slugs -> %d, want 502 (body: %s)", recNoValid.Code, recNoValid.Body.String())
	}
}

func TestLinksAIMultiTenantIsolation(t *testing.T) {
	t.Parallel()

	org1Provider := &stubLLMProvider{
		name:       "openai",
		cheapModel: "gpt-4o-mini",
		completeFn: func(context.Context, llmprovider.Request) (llmprovider.Response, error) {
			return llmprovider.Response{Text: `["org1-alpha", "org1-beta", "org1-gamma"]`}, nil
		},
	}
	org2Provider := &stubLLMProvider{
		name:       "claude",
		cheapModel: "claude-haiku",
		completeFn: func(context.Context, llmprovider.Request) (llmprovider.Response, error) {
			return llmprovider.Response{Text: `["org2-one", "org2-two", "org2-three"]`}, nil
		},
	}

	_, mux := setupLinksAIHTTP(t, func(orgID uint) (llmprovider.Provider, error) {
		switch orgID {
		case 1:
			return org1Provider, nil
		case 2:
			return org2Provider, nil
		default:
			return nil, errors.New("not configured")
		}
	})

	recStatus1 := doJSONRequest(mux, http.MethodGet, "/api/links/ai/status", "1", nil)
	var st1 LinkAIStatusOutputBody
	_ = json.Unmarshal(recStatus1.Body.Bytes(), &st1)
	if !st1.Configured || st1.Provider != "openai" {
		t.Fatalf("org 1 status = %+v, want openai", st1)
	}

	recStatus2 := doJSONRequest(mux, http.MethodGet, "/api/links/ai/status", "2", nil)
	var st2 LinkAIStatusOutputBody
	_ = json.Unmarshal(recStatus2.Body.Bytes(), &st2)
	if !st2.Configured || st2.Provider != "claude" {
		t.Fatalf("org 2 status = %+v, want claude", st2)
	}

	recStatus3 := doJSONRequest(mux, http.MethodGet, "/api/links/ai/status", "3", nil)
	var st3 LinkAIStatusOutputBody
	_ = json.Unmarshal(recStatus3.Body.Bytes(), &st3)
	if st3.Configured || st3.Provider != "" {
		t.Fatalf("org 3 status = %+v, want unconfigured", st3)
	}

	recSuggest1 := doJSONRequest(mux, http.MethodPost, "/api/links/ai/suggest-slug", "1", map[string]string{
		"target": "https://example.com/a",
	})
	if recSuggest1.Code != http.StatusOK {
		t.Fatalf("org 1 suggest -> %d", recSuggest1.Code)
	}
	var out1 LinkAISuggestSlugOutputBody
	_ = json.Unmarshal(recSuggest1.Body.Bytes(), &out1)
	if len(out1.Slugs) != 3 || out1.Slugs[0] != "org1-alpha" {
		t.Fatalf("org 1 slugs = %v, want org1-*", out1.Slugs)
	}

	recSuggest2 := doJSONRequest(mux, http.MethodPost, "/api/links/ai/suggest-slug", "2", map[string]string{
		"target": "https://example.com/a",
	})
	if recSuggest2.Code != http.StatusOK {
		t.Fatalf("org 2 suggest -> %d", recSuggest2.Code)
	}
	var out2 LinkAISuggestSlugOutputBody
	_ = json.Unmarshal(recSuggest2.Body.Bytes(), &out2)
	if len(out2.Slugs) != 3 || out2.Slugs[0] != "org2-one" {
		t.Fatalf("org 2 slugs = %v, want org2-*", out2.Slugs)
	}

	recSuggest3 := doJSONRequest(mux, http.MethodPost, "/api/links/ai/suggest-slug", "3", map[string]string{
		"target": "https://example.com/a",
	})
	if recSuggest3.Code != http.StatusBadRequest {
		t.Fatalf("org 3 suggest -> %d, want 400", recSuggest3.Code)
	}
}
