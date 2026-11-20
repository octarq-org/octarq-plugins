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

type routed struct {
	plugin       *Plugin
	handler      http.Handler
	db           *gorm.DB
	site         *fakeSite
	vault        *mockVaultService
	perms        func(r *http.Request, key, minRole string) bool
	personalOrgs map[uint]uint
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

	r := &routed{
		db: db, site: site, vault: vault,
		personalOrgs: map[uint]uint{7: 101, 8: 102},
	}
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
				UserIDFn:        func(req *http.Request) uint { return userFromHeader(req) },
				PersonalOrgIDFn: func(req *http.Request) uint { return r.personalOrgs[userFromHeader(req)] },
				RequirePermFn:   func(req *http.Request, key, minRole string) bool { return r.perms(req, key, minRole) },
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

	r.handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := plugin.WithOrgID(req.Context(), orgFromHeader(req))
		ctx = plugin.WithUserID(ctx, userFromHeader(req))
		mux.ServeHTTP(w, req.WithContext(ctx))
	})
	return r
}

func userFromHeader(r *http.Request) uint {
	if r.Header.Get("X-User-ID") == "8" {
		return 8
	}
	if r.Header.Get("X-User-ID") == "9" {
		return 9
	}
	return 7
}

func orgFromHeader(*http.Request) uint { return 1 }

func (r *routed) do(t *testing.T, method, path, userID string, body any) (*httptest.ResponseRecorder, string) {
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
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
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

	rec, body = r.do(t, http.MethodGet, "/api/pt/trackers", "8", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user 8 list -> %d %s", rec.Code, body)
	}
	if !strings.Contains(body, `"tracker_count":0`) || strings.Contains(body, `"demo"`) {
		t.Fatalf("user 8 sees user 7 data: %s", body)
	}
}

func TestRouteSameNameInTwoPersonalSpaces(t *testing.T) {
	r := newRouted(t)

	mine := createBody(r.site)
	mine["username"] = "alice"
	if rec, body := r.do(t, http.MethodPost, "/api/pt/trackers", "", mine); rec.Code != http.StatusCreated {
		t.Fatalf("user 7 create -> %d %s", rec.Code, body)
	}

	theirs := createBody(r.site)
	theirs["username"] = "bob"
	rec, body := r.do(t, http.MethodPost, "/api/pt/trackers", "8", theirs)
	if rec.Code != http.StatusCreated {
		t.Fatalf("user 8 create with the same name -> %d %s", rec.Code, body)
	}

	for _, tc := range []struct{ user, username string }{{"", "alice"}, {"8", "bob"}} {
		rec, body := r.do(t, http.MethodGet, "/api/pt/trackers", tc.user, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("user %q list -> %d %s", tc.user, rec.Code, body)
		}
		if !strings.Contains(body, `"tracker_count":1`) {
			t.Fatalf("user %q sees %s", tc.user, body)
		}
		if !strings.Contains(body, `"username":"`+tc.username+`"`) {
			t.Fatalf("user %q does not see its own row: %s", tc.user, body)
		}
	}
}

func TestRouteWithoutPersonalSpaceIsForbidden(t *testing.T) {
	r := newRouted(t)

	seed := &PTTracker{
		OwnerUserID: 7, OrgID: 1, Name: "team-row", SecretID: "seed",
		BaseURL: r.site.server.URL, Domain: "example.test", SchemaKey: "nexusphp",
	}
	if err := r.db.Create(seed).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec, body := r.do(t, http.MethodGet, "/api/pt/trackers", "9", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("list without a personal org -> %d %s", rec.Code, body)
	}
	if strings.Contains(body, "team-row") {
		t.Fatalf("the team workspace leaked into the refusal: %s", body)
	}
	if rec, body := r.do(t, http.MethodPost, "/api/pt/trackers", "9", createBody(r.site)); rec.Code != http.StatusForbidden {
		t.Fatalf("create without a personal org -> %d %s", rec.Code, body)
	}
}

func TestRouteCreateRejectsUnverifiableCookie(t *testing.T) {
	r := newRouted(t)
	body := createBody(r.site)
	delete(body, "login_secret")
	body["cookie"] = "lang=en; tpstyle=nexusphp"

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

	r.perms = func(_ *http.Request, key, _ string) bool { return key == permRead || key == permSignin }
	if rec, body := r.do(t, http.MethodGet, "/api/pt/trackers", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("read -> %d %s", rec.Code, body)
	}
	if rec, _ := r.do(t, http.MethodPost, "/api/pt/trackers", "", createBody(r.site)); rec.Code != http.StatusForbidden {
		t.Fatalf("member create -> %d", rec.Code)
	}

	r.perms = func(*http.Request, string, string) bool { return false }
	if rec, _ := r.do(t, http.MethodGet, "/api/pt/trackers", "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("unwired permission source -> %d", rec.Code)
	}
}

func TestRouteCrossOwnerDetailIsNotFound(t *testing.T) {
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
	if rec, body := r.do(t, http.MethodGet, path, "8", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner read -> %d %s", rec.Code, body)
	}
	if rec, _ := r.do(t, http.MethodDelete, path, "8", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner delete -> %d", rec.Code)
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

	rec, _ = r.do(t, http.MethodPost, "/api/pt/trackers/9999/signin", "", map[string]any{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing tracker -> %d", rec.Code)
	}
}

func TestRouteUnknownSchemaIsRefused(t *testing.T) {
	r := newRouted(t)
	body := createBody(r.site)
	body["schema"] = "not-a-schema"
	rec, response := r.do(t, http.MethodPost, "/api/pt/trackers", "", body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create -> %d %s", rec.Code, response)
	}
}

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

	var tracker PTTracker
	if err := r.db.Where("id = ?", view.ID).First(&tracker).Error; err == nil {
		t.Fatal("tracker row survived delete")
	}
}
