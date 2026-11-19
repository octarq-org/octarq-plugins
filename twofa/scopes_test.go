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

// gateByRole makes the mock session refuse member writes, exactly as the host
// does. Personal routes must not consult it.
func gateByRole(p *Plugin) {
	mHost := p.host.(*mockHost)
	mHost.session.requireRole = func(r *http.Request, min string) bool {
		return r.Header.Get("X-Role") != "member"
	}
}

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

func createVia(mux *testMux, pattern, target string, headers map[string]string, name string) *httptest.ResponseRecorder {
	return serveRequest(mux, pattern, "POST", target, map[string]any{
		"name":   name,
		"secret": "JBSWY3DPEHPK3PXP",
	}, headers, 0)
}

func TestPlugin_TeamVaultStaysAdminOnly(t *testing.T) {
	p, mux, _ := setupTestEnv(t)
	gateByRole(p)

	admin := map[string]string{"X-Role": "admin"}
	member := map[string]string{"X-Role": "member"}

	w := createVia(mux, "POST /api/twofa/accounts", "/api/twofa/accounts", admin, "Team AWS")
	if w.Code != http.StatusCreated {
		t.Fatalf("admin create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	created := decodeSummary(t, w)

	if w := createVia(mux, "POST /api/twofa/accounts", "/api/twofa/accounts", member, "Team AWS"); w.Code != http.StatusForbidden {
		t.Fatalf("member create: expected 403, got %d (%s)", w.Code, w.Body.String())
	}

	w = serveRequest(mux, "GET /api/twofa/accounts/{id}", "GET",
		fmt.Sprintf("/api/twofa/accounts/%d?reveal=true", created.ID), nil, member, created.ID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("member reveal: expected 403, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestPlugin_PersonalVaultOwnedByCaller(t *testing.T) {
	p, mux, _ := setupTestEnv(t)
	gateByRole(p)

	member42 := map[string]string{"X-Role": "member", "X-Test-User": "42"}
	member43 := map[string]string{"X-Role": "member", "X-Test-User": "43"}

	w := createVia(mux, "POST /api/personal/twofa/accounts", "/api/personal/twofa/accounts", member42, "My GitHub")
	if w.Code != http.StatusCreated {
		t.Fatalf("personal create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	created := decodeSummary(t, w)
	if created.ID == 0 {
		t.Fatalf("personal create returned no id: %+v", created)
	}

	w = serveRequest(mux, "GET /api/personal/twofa/accounts", "GET", "/api/personal/twofa/accounts", nil, member42, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("personal list: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := decodeSummaries(t, w); len(got) != 1 || got[0].Name != "My GitHub" {
		t.Fatalf("personal list: expected the caller's item, got %+v", got)
	}

	w = serveRequest(mux, "GET /api/personal/twofa/accounts/{id}", "GET",
		fmt.Sprintf("/api/personal/twofa/accounts/%d?reveal=true", created.ID), nil, member42, created.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("personal reveal: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var revealed map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &revealed)
	if revealed["secret"] != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("personal reveal: expected the stored secret, got %v", revealed["secret"])
	}

	// Another member has a different personal org: nothing to list, nothing to fetch.
	w = serveRequest(mux, "GET /api/personal/twofa/accounts", "GET", "/api/personal/twofa/accounts", nil, member43, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("other member list: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := decodeSummaries(t, w); len(got) != 0 {
		t.Fatalf("other member list: expected 0 items, got %+v", got)
	}

	w = serveRequest(mux, "GET /api/personal/twofa/accounts/{id}", "GET",
		fmt.Sprintf("/api/personal/twofa/accounts/%d", created.ID), nil, member43, created.ID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("other member fetch: expected 404, got %d (%s)", w.Code, w.Body.String())
	}

	// The personal item never appears in the team vault.
	w = serveRequest(mux, "GET /api/twofa/accounts", "GET", "/api/twofa/accounts", nil, member42, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("team list: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := decodeSummaries(t, w); len(got) != 0 {
		t.Fatalf("team list: expected 0 items, got %+v", got)
	}
}

// The owner check is the only boundary between two members who share a
// personal org row: the org lookup alone would let them read each other.
func TestPlugin_PersonalVaultRejectsForeignOwnerInSameOrg(t *testing.T) {
	p, mux, _ := setupTestEnv(t)
	gateByRole(p)

	vs, err := p.vault()
	if err != nil {
		t.Fatalf("vault unavailable: %v", err)
	}
	foreign, err := vs.PutSecret(context.Background(), plugin.PutSecretRequest{
		OrgID:          100, // the personal org the mock assigns to user 42
		Scope:          plugin.VaultScopeUser,
		OwnerUserID:    99,
		CollectionSlug: "default",
		ItemType:       "twofa_totp",
		Name:           "Someone Else",
		Metadata:       map[string]any{"issuer": "Other", "digits": 6, "period": 30, "algorithm": "SHA1"},
		Payload:        map[string]any{"secret": "JBSWY3DPEHPK3PXP"},
	})
	if err != nil {
		t.Fatalf("seed foreign personal item: %v", err)
	}

	member42 := map[string]string{"X-Role": "member", "X-Test-User": "42"}

	w := serveRequest(mux, "GET /api/personal/twofa/accounts", "GET", "/api/personal/twofa/accounts", nil, member42, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := decodeSummaries(t, w); len(got) != 0 {
		t.Fatalf("list: expected 0 items, got %+v", got)
	}

	idPath := fmt.Sprintf("/api/personal/twofa/accounts/%d", foreign.ID)
	cases := []struct {
		pattern string
		method  string
		target  string
		body    any
	}{
		{"GET /api/personal/twofa/accounts/{id}", "GET", idPath, nil},
		{"GET /api/personal/twofa/accounts/{id}", "GET", idPath + "?reveal=true", nil},
		{"PUT /api/personal/twofa/accounts/{id}", "PUT", idPath, map[string]any{"name": "Tampered"}},
		{"DELETE /api/personal/twofa/accounts/{id}", "DELETE", idPath, nil},
		{"POST /api/personal/twofa/accounts/{id}/pin", "POST", idPath + "/pin", nil},
		{"POST /api/personal/twofa/accounts/{id}/code", "POST", idPath + "/code", nil},
		{"POST /api/personal/twofa/accounts/{id}/verify", "POST", idPath + "/verify", map[string]string{"code": "123456"}},
		{"GET /api/personal/twofa/accounts/{id}/qr", "GET", idPath + "/qr", nil},
	}
	for _, tc := range cases {
		w := serveRequest(mux, tc.pattern, tc.method, tc.target, tc.body, member42, foreign.ID)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s: expected 404, got %d (%s)", tc.method, tc.target, w.Code, w.Body.String())
		}
	}
}

func TestPlugin_PersonalVaultFailsClosedWithoutPersonalOrg(t *testing.T) {
	_, mux, _ := setupTestEnv(t)
	headers := map[string]string{"X-Role": "member", "X-Test-User": "42", "X-Test-Personal-Org": "0"}

	cases := []struct {
		pattern string
		method  string
		target  string
		body    any
	}{
		{"GET /api/personal/twofa/accounts", "GET", "/api/personal/twofa/accounts", nil},
		{"POST /api/personal/twofa/accounts", "POST", "/api/personal/twofa/accounts", map[string]any{"name": "x", "secret": "JBSWY3DPEHPK3PXP"}},
		{"GET /api/personal/twofa/accounts/{id}", "GET", "/api/personal/twofa/accounts/1", nil},
		{"POST /api/personal/twofa/import", "POST", "/api/personal/twofa/import", map[string]any{"uris": []string{"otpauth://totp/x?secret=JBSWY3DPEHPK3PXP"}}},
		{"GET /api/personal/twofa/export", "GET", "/api/personal/twofa/export", nil},
		{"GET /api/personal/twofa/logs", "GET", "/api/personal/twofa/logs", nil},
	}
	for _, tc := range cases {
		w := serveRequest(mux, tc.pattern, tc.method, tc.target, tc.body, headers, 1)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s: expected 403, got %d (%s)", tc.method, tc.target, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Personal space is unavailable for this account.") {
			t.Fatalf("%s %s: missing fail-closed message: %s", tc.method, tc.target, w.Body.String())
		}
	}
}

func TestPlugin_ScopesDoNotLeak(t *testing.T) {
	p, mux, _ := setupTestEnv(t)

	admin := map[string]string{"X-Role": "admin"}
	member42 := map[string]string{"X-Role": "member", "X-Test-User": "42"}

	w := createVia(mux, "POST /api/twofa/accounts", "/api/twofa/accounts", admin, "Team Only")
	if w.Code != http.StatusCreated {
		t.Fatalf("team create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	teamItem := decodeSummary(t, w)

	w = createVia(mux, "POST /api/personal/twofa/accounts", "/api/personal/twofa/accounts", member42, "Personal Only")
	if w.Code != http.StatusCreated {
		t.Fatalf("personal create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	personalItem := decodeSummary(t, w)

	w = serveRequest(mux, "GET /api/twofa/accounts", "GET", "/api/twofa/accounts", nil, admin, 0)
	if got := decodeSummaries(t, w); len(got) != 1 || got[0].Name != "Team Only" {
		t.Fatalf("team list: expected only the team item, got %+v", got)
	}

	w = serveRequest(mux, "GET /api/personal/twofa/accounts", "GET", "/api/personal/twofa/accounts", nil, member42, 0)
	if got := decodeSummaries(t, w); len(got) != 1 || got[0].Name != "Personal Only" {
		t.Fatalf("personal list: expected only the personal item, got %+v", got)
	}

	// Cross-scope fetches are not found.
	w = serveRequest(mux, "GET /api/personal/twofa/accounts/{id}", "GET",
		fmt.Sprintf("/api/personal/twofa/accounts/%d", teamItem.ID), nil, member42, teamItem.ID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("personal fetch of team item: expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	w = serveRequest(mux, "GET /api/twofa/accounts/{id}", "GET",
		fmt.Sprintf("/api/twofa/accounts/%d", personalItem.ID), nil, admin, personalItem.ID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("team fetch of personal item: expected 404, got %d (%s)", w.Code, w.Body.String())
	}

	// An org-scoped item that happens to live in the personal org stays out of
	// the personal vault: scope, not just org, is the boundary.
	vs, err := p.vault()
	if err != nil {
		t.Fatalf("vault unavailable: %v", err)
	}
	orgScoped, err := vs.PutSecret(context.Background(), plugin.PutSecretRequest{
		OrgID:          100,
		Scope:          plugin.VaultScopeOrg,
		CollectionSlug: "default",
		ItemType:       "twofa_totp",
		Name:           "Org Scoped In Personal Org",
		Metadata:       map[string]any{"digits": 6, "period": 30, "algorithm": "SHA1"},
		Payload:        map[string]any{"secret": "JBSWY3DPEHPK3PXP"},
	})
	if err != nil {
		t.Fatalf("seed org-scoped item: %v", err)
	}

	w = serveRequest(mux, "GET /api/personal/twofa/accounts", "GET", "/api/personal/twofa/accounts", nil, member42, 0)
	if got := decodeSummaries(t, w); len(got) != 1 || got[0].Name != "Personal Only" {
		t.Fatalf("personal list: expected the org-scoped item to stay out, got %+v", got)
	}
	w = serveRequest(mux, "GET /api/personal/twofa/accounts/{id}", "GET",
		fmt.Sprintf("/api/personal/twofa/accounts/%d", orgScoped.ID), nil, member42, orgScoped.ID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("personal fetch of org-scoped item: expected 404, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestPlugin_PersonalAuditStaysInPersonalOrg(t *testing.T) {
	_, mux, _ := setupTestEnv(t)

	admin := map[string]string{"X-Role": "admin"}
	member42 := map[string]string{"X-Role": "member", "X-Test-User": "42"}

	w := createVia(mux, "POST /api/personal/twofa/accounts", "/api/personal/twofa/accounts", member42, "Audited Personal")
	if w.Code != http.StatusCreated {
		t.Fatalf("personal create: expected 201, got %d (%s)", w.Code, w.Body.String())
	}
	created := decodeSummary(t, w)

	w = serveRequest(mux, "GET /api/personal/twofa/accounts/{id}", "GET",
		fmt.Sprintf("/api/personal/twofa/accounts/%d?reveal=true", created.ID), nil, member42, created.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("personal reveal: expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	w = serveRequest(mux, "GET /api/personal/twofa/logs", "GET", "/api/personal/twofa/logs", nil, member42, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("personal logs: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var personalLogs []TwoFAAuditLog
	_ = json.Unmarshal(w.Body.Bytes(), &personalLogs)
	var sawReveal bool
	for _, entry := range personalLogs {
		if entry.Action == "view_secret" && entry.AccountName == "Audited Personal" {
			sawReveal = true
			if entry.OrgID != 100 {
				t.Fatalf("personal audit row: expected org 100, got %d", entry.OrgID)
			}
		}
	}
	if !sawReveal {
		t.Fatalf("personal audit: expected a view_secret row, got %+v", personalLogs)
	}

	w = serveRequest(mux, "GET /api/twofa/logs", "GET", "/api/twofa/logs", nil, admin, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("team logs: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var teamLogs []TwoFAAuditLog
	_ = json.Unmarshal(w.Body.Bytes(), &teamLogs)
	if len(teamLogs) != 0 {
		t.Fatalf("team audit: expected no personal rows, got %+v", teamLogs)
	}
}
