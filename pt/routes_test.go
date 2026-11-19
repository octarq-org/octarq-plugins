package pt

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

// routed is a plugin mounted on a live huma API with an org-stamping front door.
type routed struct {
	plugin  *Plugin
	handler http.Handler
	db      *gorm.DB
	site    *fakeSite
	vault   *mockVaultService
	perms   func(r *http.Request, key, minRole string) bool
}

func newRouted(t *testing.T) *routed {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&PTTracker{}, &PTSnapshot{}, &PTSigninResult{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	site := newFakeSite(t)
	vault := newMockVaultService()
	reg := plugin.NewRegistry()
	reg.Provide(plugin.ServiceVault, plugin.VaultService(vault))

	r := &routed{db: db, site: site, vault: vault}
	r.perms = func(*http.Request, string, string) bool { return true }

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("pt test", "1.0.0"))

	r.plugin = &Plugin{
		newClient: func(time.Duration) *http.Client { return site.server.Client() },
		now:       func() time.Time { return time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC) },
	}
	r.plugin.Mount(mux, &plugin.Context{
		DB:      db,
		Huma:    api,
		Guard:   func(h http.Handler) http.Handler { return h },
		Provide: reg.Provide,
		Lookup:  reg.Lookup,
		Host: &plugin.TestHost{
			SessionMock: &plugin.TestSession{
				UserIDFn:      func(*http.Request) uint { return 7 },
				RequirePermFn: func(req *http.Request, key, minRole string) bool { return r.perms(req, key, minRole) },
			},
			TenantDBMock: func(orgID uint) *plugin.TenantDB {
				tdb, err := plugin.NewTenantDB(db, orgID)
				if err != nil {
					return nil
				}
				return tdb
			},
		},
	})

	// Every real request arrives with the tenant already resolved. The test
	// front door stands in for internal/auth.
	r.handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := plugin.WithOrgID(req.Context(), orgFromHeader(req))
		ctx = plugin.WithUserID(ctx, 7)
		mux.ServeHTTP(w, req.WithContext(ctx))
	})
	return r
}

func orgFromHeader(r *http.Request) uint {
	switch r.Header.Get("X-Org-ID") {
	case "2":
		return 2
	case "":
		return 1
	default:
		return 1
	}
}

func (r *routed) do(t *testing.T, method, path, org string, body any) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if org != "" {
		req.Header.Set("X-Org-ID", org)
	}
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	return rec, rec.Body.String()
}

func createBody(site *fakeSite) map[string]any {
	return map[string]any{
		"name":         "demo",
		"schema":       "nexusphp",
		"base_url":     site.server.URL,
		"username":     "alice",
		"login_secret": "secret",
	}
}

func TestRouteCreateAndList(t *testing.T) {
	r := newRouted(t)

	rec, body := r.do(t, http.MethodPost, "/api/pt/trackers", "", createBody(r.site))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create -> %d %s", rec.Code, body)
	}
	if strings.Contains(body, "c_secure_uid") || strings.Contains(body, "ciphertext") {
		t.Fatalf("the response leaked stored material: %s", body)
	}
	if !strings.Contains(body, `"schema_label"`) {
		t.Fatalf("create response is not a tracker view: %s", body)
	}

	rec, body = r.do(t, http.MethodGet, "/api/pt/trackers", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list -> %d %s", rec.Code, body)
	}
	if !strings.Contains(body, `"demo"`) || !strings.Contains(body, `"tracker_count":1`) {
		t.Fatalf("list body = %s", body)
	}

	// Another workspace sees an empty list and its own summary.
	rec, body = r.do(t, http.MethodGet, "/api/pt/trackers", "2", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("org 2 list -> %d %s", rec.Code, body)
	}
	if !strings.Contains(body, `"tracker_count":0`) || strings.Contains(body, `"demo"`) {
		t.Fatalf("org 2 sees workspace 1 data: %s", body)
	}
}

func TestRouteCreateRejectsUnverifiableCookie(t *testing.T) {
	r := newRouted(t)
	body := createBody(r.site)
	delete(body, "login_secret")
	body["cookie"] = "lang=en; tpstyle=nexusphp"

	// The fake site serves an anonymous page for a bare cookie, so the check
	// fails and nothing may be written.
	rec, response := r.do(t, http.MethodPost, "/api/pt/trackers", "", body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create -> %d %s", rec.Code, response)
	}

	var count int64
	if err := r.db.Model(&PTTracker{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("a rejected cookie still created %d trackers", count)
	}
}

func TestRoutePermissionsFailClosed(t *testing.T) {
	r := newRouted(t)

	// A member may read and sign in but not create.
	r.perms = func(_ *http.Request, key, _ string) bool { return key == permRead || key == permSignin }
	if rec, body := r.do(t, http.MethodGet, "/api/pt/trackers", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("read -> %d %s", rec.Code, body)
	}
	if rec, _ := r.do(t, http.MethodPost, "/api/pt/trackers", "", createBody(r.site)); rec.Code != http.StatusForbidden {
		t.Fatalf("member create -> %d", rec.Code)
	}

	// No permission source at all must refuse, never allow.
	r.perms = func(*http.Request, string, string) bool { return false }
	if rec, _ := r.do(t, http.MethodGet, "/api/pt/trackers", "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("unwired permission source -> %d", rec.Code)
	}
}

func TestRouteCrossTenantDetailIsNotFound(t *testing.T) {
	r := newRouted(t)
	rec, body := r.do(t, http.MethodPost, "/api/pt/trackers", "", createBody(r.site))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create -> %d %s", rec.Code, body)
	}

	var view struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	path := fmt.Sprintf("/api/pt/trackers/%d", view.ID)

	if rec, body := r.do(t, http.MethodGet, path, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("owner read -> %d %s", rec.Code, body)
	}
	// Absent, not forbidden: a 403 would confirm the id exists.
	if rec, body := r.do(t, http.MethodGet, path, "2", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace read -> %d %s", rec.Code, body)
	}
	if rec, _ := r.do(t, http.MethodDelete, path, "2", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace delete -> %d", rec.Code)
	}
}

func TestRouteSigninWritesLedger(t *testing.T) {
	r := newRouted(t)
	rec, body := r.do(t, http.MethodPost, "/api/pt/trackers", "", createBody(r.site))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create -> %d %s", rec.Code, body)
	}
	var view struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}

	path := fmt.Sprintf("/api/pt/trackers/%d/signin", view.ID)
	rec, body = r.do(t, http.MethodPost, path, "", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("signin -> %d %s", rec.Code, body)
	}
	if !strings.Contains(body, `"already_signed"`) {
		t.Fatalf("signin outcome = %s", body)
	}

	// A missing tracker is a 404, not a 500.
	rec, _ = r.do(t, http.MethodPost, "/api/pt/trackers/9999/signin", "", map[string]any{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing tracker -> %d", rec.Code)
	}
}

// TestRouteUnknownSchemaIsRefused guards the registry boundary: the schema key
// comes from the client and must be one the build registered.
func TestRouteUnknownSchemaIsRefused(t *testing.T) {
	r := newRouted(t)
	body := createBody(r.site)
	body["schema"] = "not-a-schema"
	rec, response := r.do(t, http.MethodPost, "/api/pt/trackers", "", body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create -> %d %s", rec.Code, response)
	}
}

// TestRouteHandlersResolveRequest keeps the huma input wiring honest: an
// untouched request still reaches the handler with the caller's workspace.
func TestRouteHandlersResolveRequest(t *testing.T) {
	r := newRouted(t)
	rec, body := r.do(t, http.MethodGet, "/api/pt/overview", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview -> %d %s", rec.Code, body)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := view["scheduler_available"]; !ok {
		t.Fatalf("overview body = %s", body)
	}
	if _, ok := view["server_timezone"]; !ok {
		t.Fatalf("overview body = %s", body)
	}
}

// TestRouteDeleteShredsCredential proves the delete path removes the rows and
// then destroys the subject key.
func TestRouteDeleteShredsCredential(t *testing.T) {
	r := newRouted(t)
	rec, body := r.do(t, http.MethodPost, "/api/pt/trackers", "", createBody(r.site))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create -> %d %s", rec.Code, body)
	}
	var view struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var before PTTracker
	if err := r.db.Where("id = ?", view.ID).First(&before).Error; err != nil {
		t.Fatalf("load tracker before delete: %v", err)
	}

	path := fmt.Sprintf("/api/pt/trackers/%d", view.ID)
	if rec, body := r.do(t, http.MethodDelete, path, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete -> %d %s", rec.Code, body)
	}

	if _, err := r.vault.GetSecret(t.Context(), 1, before.VaultItemID); !errors.Is(err, plugin.ErrSecretNotFound) {
		t.Fatalf("expected vault secret to be shredded after delete, got %v", err)
	}

	// The tracker row is gone with it.
	var tracker PTTracker
	if err := r.db.Where("id = ?", view.ID).First(&tracker).Error; err == nil {
		t.Fatal("tracker row survived delete")
	}
}
