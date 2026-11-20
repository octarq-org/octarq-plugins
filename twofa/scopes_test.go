package twofa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/octarq-org/octarq/server/plugin"
)

func serveRequest(mux *testMux, pattern, method, target string, body any, headers map[string]string, id uint) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	var req *http.Request
	if reader != nil {
		req = httptest.NewRequest(method, target, reader)
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if id != 0 {
		req.SetPathValue("id", strconv.FormatUint(uint64(id), 10))
	}
	w := httptest.NewRecorder()
	mux.Serve(pattern, w, req)
	return w
}

func decodeSummary(t *testing.T, w *httptest.ResponseRecorder) AccountSummary {
	t.Helper()
	var out AccountSummary
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode account summary: %v (%s)", err, w.Body.String())
	}
	return out
}

func decodeSummaries(t *testing.T, w *httptest.ResponseRecorder) []AccountSummary {
	t.Helper()
	var out []AccountSummary
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode account summaries: %v (%s)", err, w.Body.String())
	}
	return out
}

func createVia(mux *testMux, target string, headers map[string]string, name string) *httptest.ResponseRecorder {
	return serveRequest(mux, "POST /api/twofa/accounts", "POST", target, map[string]any{
		"name":   name,
		"secret": "JBSWY3DPEHPK3PXP",
	}, headers, 0)
}

func setShareVia(mux *testMux, headers map[string]string, id uint, shared bool) *httptest.ResponseRecorder {
	return serveRequest(mux, "POST /api/twofa/accounts/{id}/share", "POST",
		fmt.Sprintf("/api/twofa/accounts/%d/share", id), map[string]any{"shared": shared}, headers, id)
}

var (
	owner42  = map[string]string{"X-Role": "member", "X-Test-User": "42"}
	member43 = map[string]string{"X-Role": "member", "X-Test-User": "43"}
)

func TestPlugin_PersonalVaultOwnedByCaller(t *testing.T) {
	_, mux, _ := setupTestEnv(t)

	w := createVia(mux, "/api/twofa/accounts", owner42, "My GitHub")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	created := decodeSummary(t, w)
	if created.ID == 0 || !created.Mine || created.Shared {
		t.Fatalf("create: expected an unshared owned account, got %+v", created)
	}

	w = serveRequest(mux, "GET /api/twofa/accounts", "GET", "/api/twofa/accounts", nil, owner42, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("owner list: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := decodeSummaries(t, w); len(got) != 1 || got[0].Name != "My GitHub" || !got[0].Mine || got[0].Shared {
		t.Fatalf("owner list: expected the unshared owned item, got %+v", got)
	}

	w = serveRequest(mux, "GET /api/twofa/accounts/{id}", "GET",
		fmt.Sprintf("/api/twofa/accounts/%d?reveal=true", created.ID), nil, owner42, created.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("owner reveal: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var revealed map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &revealed)
	if revealed["secret"] != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("owner reveal: expected the stored secret, got %v", revealed["secret"])
	}

	w = serveRequest(mux, "GET /api/twofa/accounts", "GET", "/api/twofa/accounts", nil, member43, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("other member list: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := decodeSummaries(t, w); len(got) != 0 {
		t.Fatalf("other member list: expected 0 items, got %+v", got)
	}

	w = serveRequest(mux, "GET /api/twofa/accounts/{id}", "GET",
		fmt.Sprintf("/api/twofa/accounts/%d", created.ID), nil, member43, created.ID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("other member fetch: expected 404, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestPlugin_OwnerOnlyOperationsReturn404ForOtherMembers(t *testing.T) {
	_, mux, _ := setupTestEnv(t)

	w := createVia(mux, "/api/twofa/accounts", owner42, "Owner Only")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	created := decodeSummary(t, w)
	idPath := fmt.Sprintf("/api/twofa/accounts/%d", created.ID)

	cases := []struct {
		pattern string
		method  string
		target  string
		body    any
	}{
		{"GET /api/twofa/accounts/{id}", "GET", idPath, nil},
		{"GET /api/twofa/accounts/{id}", "GET", idPath + "?reveal=true", nil},
		{"PUT /api/twofa/accounts/{id}", "PUT", idPath, map[string]any{"name": "Tampered"}},
		{"DELETE /api/twofa/accounts/{id}", "DELETE", idPath, nil},
		{"POST /api/twofa/accounts/{id}/pin", "POST", idPath + "/pin", nil},
		{"GET /api/twofa/accounts/{id}/qr", "GET", idPath + "/qr", nil},
		{"POST /api/twofa/accounts/{id}/share", "POST", idPath + "/share", map[string]any{"shared": true}},
	}
	for _, tc := range cases {
		w := serveRequest(mux, tc.pattern, tc.method, tc.target, tc.body, member43, created.ID)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s: expected 404, got %d (%s)", tc.method, tc.target, w.Code, w.Body.String())
		}
	}
}

func TestPlugin_SharedItemVisibleAndCodeReadable(t *testing.T) {
	p, mux, _ := setupTestEnv(t)

	w := createVia(mux, "/api/twofa/accounts", owner42, "Shared AWS")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	created := decodeSummary(t, w)

	w = setShareVia(mux, owner42, created.ID, true)
	if w.Code != http.StatusOK {
		t.Fatalf("share: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var shareResp map[string]bool
	_ = json.Unmarshal(w.Body.Bytes(), &shareResp)
	if !shareResp["shared"] {
		t.Fatalf("share: expected shared=true, got %+v", shareResp)
	}

	w = serveRequest(mux, "GET /api/twofa/accounts", "GET", "/api/twofa/accounts", nil, owner42, 0)
	if got := decodeSummaries(t, w); len(got) != 1 || !got[0].Mine || !got[0].Shared {
		t.Fatalf("owner list: expected mine=true shared=true, got %+v", got)
	}

	w = serveRequest(mux, "GET /api/twofa/accounts", "GET", "/api/twofa/accounts", nil, member43, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("shared list: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	list := decodeSummaries(t, w)
	if len(list) != 1 || list[0].Name != "Shared AWS" || list[0].Mine || !list[0].Shared {
		t.Fatalf("shared list: expected one mine=false shared=true item, got %+v", list)
	}
	if len(list[0].CurrentCode) != 6 {
		t.Fatalf("shared list: expected a live code, got %q", list[0].CurrentCode)
	}

	w = serveRequest(mux, "POST /api/twofa/accounts/{id}/code", "POST",
		fmt.Sprintf("/api/twofa/accounts/%d/code", created.ID), nil, member43, created.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("shared code: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var codeResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &codeResp)
	code, _ := codeResp["code"].(string)
	if len(code) != 6 {
		t.Fatalf("shared code: expected a 6-digit code, got %v", codeResp)
	}

	w = serveRequest(mux, "POST /api/twofa/accounts/{id}/verify", "POST",
		fmt.Sprintf("/api/twofa/accounts/%d/verify", created.ID), map[string]string{"code": code}, member43, created.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("shared verify: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var verifyResp map[string]bool
	_ = json.Unmarshal(w.Body.Bytes(), &verifyResp)
	if !verifyResp["valid"] {
		t.Fatalf("shared verify: expected the live code to be valid")
	}

	for _, tc := range []struct {
		pattern string
		method  string
		target  string
		body    any
	}{
		{"GET /api/twofa/accounts/{id}", "GET", fmt.Sprintf("/api/twofa/accounts/%d?reveal=true", created.ID), nil},
		{"PUT /api/twofa/accounts/{id}", "PUT", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), map[string]any{"name": "Tampered"}},
		{"DELETE /api/twofa/accounts/{id}", "DELETE", fmt.Sprintf("/api/twofa/accounts/%d", created.ID), nil},
		{"POST /api/twofa/accounts/{id}/pin", "POST", fmt.Sprintf("/api/twofa/accounts/%d/pin", created.ID), nil},
		{"GET /api/twofa/accounts/{id}/qr", "GET", fmt.Sprintf("/api/twofa/accounts/%d/qr", created.ID), nil},
	} {
		w := serveRequest(mux, tc.pattern, tc.method, tc.target, tc.body, member43, created.ID)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s: expected 404 for a non-owner, got %d (%s)", tc.method, tc.target, w.Code, w.Body.String())
		}
	}

	w = setShareVia(mux, owner42, created.ID, false)
	if w.Code != http.StatusOK {
		t.Fatalf("unshare: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	w = serveRequest(mux, "GET /api/twofa/accounts", "GET", "/api/twofa/accounts", nil, member43, 0)
	if got := decodeSummaries(t, w); len(got) != 0 {
		t.Fatalf("after unshare: expected 0 items, got %+v", got)
	}

	if _, _, err := p.GetCode(context.Background(), 1, "Shared AWS"); err == nil {
		t.Fatalf("provider GetCode after unshare: expected an error")
	}
}

func TestPlugin_ShareToggleOwnerOnly(t *testing.T) {
	_, mux, _ := setupTestEnv(t)

	w := createVia(mux, "/api/twofa/accounts", owner42, "Owned")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	created := decodeSummary(t, w)

	w = setShareVia(mux, member43, created.ID, true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner share: expected 404, got %d (%s)", w.Code, w.Body.String())
	}

	w = setShareVia(mux, owner42, created.ID, true)
	if w.Code != http.StatusOK {
		t.Fatalf("owner share: expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	w = setShareVia(mux, owner42, created.ID, true)
	if w.Code != http.StatusOK {
		t.Fatalf("owner share again: expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	w = setShareVia(mux, owner42, created.ID, false)
	if w.Code != http.StatusOK {
		t.Fatalf("owner unshare: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestPlugin_MCPListsSharedOnly(t *testing.T) {
	p, mux, _ := setupTestEnv(t)

	w := createVia(mux, "/api/twofa/accounts", owner42, "Shared Service")
	if w.Code != http.StatusCreated {
		t.Fatalf("create shared: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	shared := decodeSummary(t, w)
	if w := setShareVia(mux, owner42, shared.ID, true); w.Code != http.StatusOK {
		t.Fatalf("share: expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	w = createVia(mux, "/api/twofa/accounts", owner42, "Private Service")
	if w.Code != http.StatusCreated {
		t.Fatalf("create private: expected 201, got %d (%s)", w.Code, w.Body.String())
	}

	mcpCtx := plugin.WithOrgID(context.Background(), 1)
	res, raw, err := p.mcpListAccounts(mcpCtx, nil, listAccountsInput{})
	if err != nil {
		t.Fatalf("mcpListAccounts failed: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("mcpListAccounts returned no content")
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal mcp accounts: %v", err)
	}
	var items []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(buf, &items); err != nil {
		t.Fatalf("decode mcp accounts: %v", err)
	}
	if len(items) != 1 || items[0].Name != "Shared Service" {
		t.Fatalf("mcpListAccounts: expected only the shared account, got %+v", items)
	}

	if _, _, err := p.mcpGetCode(mcpCtx, nil, getCodeInput{AccountName: "Shared Service"}); err != nil {
		t.Fatalf("mcpGetCode shared: %v", err)
	}
	if _, _, err := p.mcpGetCode(mcpCtx, nil, getCodeInput{AccountName: "Private Service"}); err == nil {
		t.Fatalf("mcpGetCode private: expected an error")
	}
}

func TestPlugin_ExportOnlyOwnItems(t *testing.T) {
	_, mux, _ := setupTestEnv(t)

	w := createVia(mux, "/api/twofa/accounts", member43, "Member Item")
	if w.Code != http.StatusCreated {
		t.Fatalf("create member item: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	memberItem := decodeSummary(t, w)
	if w := setShareVia(mux, member43, memberItem.ID, true); w.Code != http.StatusOK {
		t.Fatalf("share member item: expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	w = createVia(mux, "/api/twofa/accounts", owner42, "Owner Item")
	if w.Code != http.StatusCreated {
		t.Fatalf("create owner item: expected 201, got %d (%s)", w.Code, w.Body.String())
	}

	w = serveRequest(mux, "GET /api/twofa/export", "GET", "/api/twofa/export", nil, owner42, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var exported []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &exported)
	if len(exported) != 1 || exported[0]["name"] != "Owner Item" {
		t.Fatalf("export: expected only the caller's item, got %+v", exported)
	}
}

func TestPlugin_ResolveSharedItemRejectsAmbiguousName(t *testing.T) {
	p, mux, _ := setupTestEnv(t)

	for i := 0; i < 2; i++ {
		w := createVia(mux, "/api/twofa/accounts", owner42, "Duplicate Name")
		if w.Code != http.StatusCreated {
			t.Fatalf("create %d: expected 201, got %d (%s)", i, w.Code, w.Body.String())
		}
		created := decodeSummary(t, w)
		if w := setShareVia(mux, owner42, created.ID, true); w.Code != http.StatusOK {
			t.Fatalf("share %d: expected 200, got %d (%s)", i, w.Code, w.Body.String())
		}
	}

	if _, _, err := p.GetCode(context.Background(), 1, "Duplicate Name"); err == nil {
		t.Fatal("provider GetCode: expected an error for an ambiguous name")
	} else if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("provider GetCode: expected an ambiguous error, got %v", err)
	}

	mcpCtx := plugin.WithOrgID(context.Background(), 1)
	if _, _, err := p.mcpGetCode(mcpCtx, nil, getCodeInput{AccountName: "Duplicate Name"}); err == nil {
		t.Fatal("mcpGetCode: expected an error for an ambiguous name")
	} else if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("mcpGetCode: expected an ambiguous error, got %v", err)
	}
}

func TestPlugin_DeleteRemovesShareBeforeVaultItem(t *testing.T) {
	p, mux, db := setupTestEnv(t)

	w := createVia(mux, "/api/twofa/accounts", owner42, "Ordered Delete")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	created := decodeSummary(t, w)
	if w := setShareVia(mux, owner42, created.ID, true); w.Code != http.StatusOK {
		t.Fatalf("share: expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	if err := db.Migrator().DropTable(&TwoFAShare{}); err != nil {
		t.Fatalf("drop share table: %v", err)
	}

	w = serveRequest(mux, "DELETE /api/twofa/accounts/{id}", "DELETE",
		fmt.Sprintf("/api/twofa/accounts/%d", created.ID), nil, owner42, created.ID)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("delete: expected 500 when share removal fails, got %d (%s)", w.Code, w.Body.String())
	}

	vs, err := p.vault()
	if err != nil {
		t.Fatalf("vault unavailable: %v", err)
	}
	if _, err := vs.GetMeta(context.Background(), 1, created.ID); err != nil {
		t.Fatalf("delete order: expected the vault item to remain after a failed share delete, got %v", err)
	}
}
