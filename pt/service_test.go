package pt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

type fakeSession struct{}

func (fakeSession) UserID(*http.Request) uint                      { return 7 }
func (fakeSession) OrgID(*http.Request) uint                       { return 1 }
func (fakeSession) PersonalOrgID(*http.Request) uint               { return 1 }
func (fakeSession) OrgRole(*http.Request) string                   { return "admin" }
func (fakeSession) RequireRole(*http.Request, string) bool         { return true }
func (fakeSession) RequirePerm(*http.Request, string, string) bool { return true }
func (fakeSession) IsInstanceAdmin(*http.Request) bool             { return true }
func (fakeSession) RevokeUserOrgSessions(uint, uint) int           { return 0 }

type fakeHost struct {
	plugin.Host
	db *gorm.DB
}

func (h *fakeHost) Session() plugin.HostSession { return fakeSession{} }

func (h *fakeHost) TenantDB(orgID uint) *plugin.TenantDB {
	tdb, err := plugin.NewTenantDB(h.db, orgID)
	if err != nil {
		return nil
	}
	return tdb
}

type fakeSite struct {
	server         *httptest.Server
	attendance     atomic.Int32
	logins         atomic.Int32
	hits           atomic.Int32
	attendanceBody string
}

func newFakeSite(t *testing.T) *fakeSite {
	t.Helper()
	f := &fakeSite{attendanceBody: `<html><body><a href="/logout.php">Logout</a><p>今天已签到</p></body></html>`}

	const indexPage = `<html><body>
<a href="/logout.php">Logout</a>
<a href="/userdetails.php?id=42">alice</a>
<a href="/mybonus.php">魔力值 100</a>
<p>上传量 1 TB</p>
<p>下载量 500 GB</p>
<p>分享率 2.0</p>
</body></html>`
	const anonymousPage = `<html><body>
<form action="/takelogin.php" method="post">
  <input type="text" name="` + "username" + `" />
  <input type="password" name="` + "password" + `" />
</form>
</body></html>`

	authed := func(r *http.Request) bool {
		c, err := r.Cookie("c_secure_uid")
		return err == nil && c.Value == "42"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/login.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(anonymousPage))
	})
	mux.HandleFunc("/takelogin.php", func(w http.ResponseWriter, r *http.Request) {
		f.logins.Add(1)
		_ = r.ParseForm()
		if r.PostForm.Get("username") != "alice" || r.PostForm.Get("password") != "secret" {
			_, _ = w.Write([]byte(anonymousPage))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "c_secure_uid", Value: "42", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "c_secure_pass", Value: "hash", Path: "/"})
		w.Header().Set("Location", "/index.php")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/index.php", func(w http.ResponseWriter, r *http.Request) {
		if authed(r) {
			_, _ = w.Write([]byte(indexPage))
			return
		}
		_, _ = w.Write([]byte(anonymousPage))
	})
	mux.HandleFunc("/userdetails.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><table><tr><td>等级</td><td>Power User</td></tr></table></body></html>`))
	})
	mux.HandleFunc("/attendance.php", func(w http.ResponseWriter, r *http.Request) {
		f.attendance.Add(1)
		if !authed(r) {
			_, _ = w.Write([]byte(anonymousPage))
			return
		}
		_, _ = w.Write([]byte(f.attendanceBody))
	})

	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

type fixture struct {
	plugin *Plugin
	db     *gorm.DB
	vault  *mockVaultService
	site   *fakeSite
	now    time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&PTTracker{}, &PTSnapshot{}, &PTSigninResult{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	vault := newMockVaultService()
	site := newFakeSite(t)
	host := &fakeHost{db: db}
	f := &fixture{db: db, vault: vault, site: site, now: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)}

	f.plugin = &Plugin{
		db:       db,
		host:     host,
		vaultSvc: vault,
		newClient: func(time.Duration) *http.Client {
			return site.server.Client()
		},
		now: func() time.Time { return f.now },
	}
	return f
}

func (f *fixture) createTracker(t *testing.T, orgID, ownerUserID uint, name string, secret storedSecret) *PTTracker {
	t.Helper()
	ctx := context.Background()

	secretID, err := newSecretID()
	if err != nil {
		t.Fatalf("secret id: %v", err)
	}

	tracker := &PTTracker{
		OwnerUserID:          ownerUserID,
		OrgID:                orgID,
		Name:                 name,
		SecretID:             secretID,
		BaseURL:              f.site.server.URL,
		Domain:               adapter.NormalizeDomain(f.site.server.URL),
		SchemaKey:            string(adapter.SchemaNexusPHP),
		Username:             "alice",
		UserAgent:            "test-agent",
		Enabled:              true,
		SigninEnabled:        true,
		StatsIntervalHours:   6,
		TimeoutSeconds:       5,
		LimitIntervalSeconds: 0,
		CreatedByUserID:      ownerUserID,
	}
	if err := f.plugin.createTracker(ctx, tracker); err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	if !secret.empty() {
		if err := f.plugin.storeSecrets(ctx, tracker, secret, false); err != nil {
			t.Fatalf("store secrets: %v", err)
		}
	}
	return tracker
}

func TestLoginStoresEncryptedCookie(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, 7, "demo", storedSecret{Login: "secret"})

	out, err := f.plugin.login(ctx, tracker)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if out.Outcome != string(adapter.OutcomeSuccess) || !out.CookieValid {
		t.Fatalf("outcome = %+v", out)
	}
	if f.site.logins.Load() != 1 {
		t.Fatalf("logins = %d", f.site.logins.Load())
	}

	if tracker.VaultItemID == 0 {
		t.Fatal("no vault item id stored")
	}
	meta, err := f.vault.GetMeta(ctx, 1, tracker.VaultItemID)
	if err != nil {
		t.Fatalf("vault meta: %v", err)
	}
	if meta.CurrentVersion != 2 {
		t.Fatalf("secret version = %d", meta.CurrentVersion)
	}
	if !slices.Contains(meta.FieldKeys, "cookie") {
		t.Fatal("cookie field key not recorded in vault meta")
	}

	secret, err := f.plugin.readSecrets(ctx, tracker)
	if err != nil {
		t.Fatalf("read secrets: %v", err)
	}
	if !strings.Contains(secret.Cookie, "c_secure_uid") {
		t.Fatalf("stored cookie = %q", secret.Cookie)
	}
	if secret.Login != "secret" {
		t.Fatal("login secret was lost when the cookie was persisted")
	}
}

func TestRefreshStoresSnapshot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, 7, "demo", storedSecret{Login: "secret"})

	snap, err := f.plugin.refresh(ctx, tracker, triggerManual)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if snap.UploadedBytes == nil || *snap.UploadedBytes != 1099511627776 {
		t.Fatalf("uploaded = %v", snap.UploadedBytes)
	}
	if snap.DownloadedBytes == nil || *snap.DownloadedBytes != 536870912000 {
		t.Fatalf("downloaded = %v", snap.DownloadedBytes)
	}
	if snap.UserLevel != "Power User" {
		t.Fatalf("level = %q", snap.UserLevel)
	}
	if snap.Source != triggerManual {
		t.Fatalf("source = %q", snap.Source)
	}
}

func TestSigninIsOncePerDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, 7, "demo", storedSecret{Login: "secret"})

	first, err := f.plugin.signin(ctx, tracker, triggerManual, false)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if first.Outcome != string(adapter.OutcomeAlreadySigned) {
		t.Fatalf("outcome = %q", first.Outcome)
	}
	requests := f.site.attendance.Load()

	second, err := f.plugin.signin(ctx, tracker, triggerManual, false)
	if err != nil {
		t.Fatalf("second signin: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("a second sign-in row was written: %d then %d", first.ID, second.ID)
	}
	if got := f.site.attendance.Load(); got != requests {
		t.Fatalf("the site was contacted again: %d -> %d", requests, got)
	}

	f.now = f.now.Add(10 * time.Minute)
	if _, err := f.plugin.signin(ctx, tracker, triggerManual, true); err != nil {
		t.Fatalf("forced signin: %v", err)
	}
	if got := f.site.attendance.Load(); got != requests+1 {
		t.Fatalf("forced sign-in did not contact the site: %d -> %d", requests, got)
	}
}

func TestCredentialIsBoundToWorkspaceAndTracker(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := f.createTracker(t, 1, 7, "first", storedSecret{Login: "secret"})
	second := f.createTracker(t, 1, 7, "second", storedSecret{Login: "other"})

	sec1, err := f.vault.GetSecret(ctx, 1, first.VaultItemID)
	if err != nil || sec1.Payload["login"] != "secret" {
		t.Fatalf("org 1 read first: %+v, %v", sec1, err)
	}
	if first.VaultItemID == second.VaultItemID {
		t.Fatal("trackers share the same VaultItemID")
	}
	if _, err := f.vault.GetSecret(ctx, 2, first.VaultItemID); !errors.Is(err, plugin.ErrSecretNotFound) {
		t.Fatalf("expected ErrSecretNotFound across workspaces, got %v", err)
	}
}

func TestVaultRefusesAnotherOwnersItem(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mine := f.createTracker(t, 1, 7, "mine", storedSecret{Login: "secret"})
	theirs := f.createTracker(t, 1, 8, "theirs", storedSecret{Login: "other"})

	theirs.VaultItemID = mine.VaultItemID

	sec, err := f.plugin.readVaultSecret(ctx, theirs)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("reveal of another owner's item = %v", err)
	}
	if !sec.empty() {
		t.Fatalf("another owner's payload leaked: %+v", sec)
	}

	if err := f.plugin.storeVaultSecret(ctx, theirs, storedSecret{Login: "changed"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update of another owner's item = %v", err)
	}
	if err := f.plugin.shredSecret(ctx, 1, mine.VaultItemID, 8); !errors.Is(err, ErrNotFound) {
		t.Fatalf("shred of another owner's item = %v", err)
	}

	kept, err := f.vault.GetSecret(ctx, 1, mine.VaultItemID)
	if err != nil {
		t.Fatalf("owner's item was damaged: %v", err)
	}
	if kept.Payload["login"] != "secret" {
		t.Fatalf("owner's payload changed: %+v", kept.Payload)
	}
}

func TestListTrackersIsOwnerScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.createTracker(t, 1, 7, "mine", storedSecret{Login: "secret"})
	f.createTracker(t, 1, 8, "theirs", storedSecret{Login: "other"})

	rows, err := f.plugin.listTrackers(ctx, 1, 7)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "mine" {
		t.Fatalf("owner 7 list = %+v", rows)
	}
}

func TestShredMakesCiphertextUnreadable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, 7, "demo", storedSecret{Login: "secret"})

	if err := f.plugin.shredSecret(ctx, 1, tracker.VaultItemID, 7); err != nil {
		t.Fatalf("shred: %v", err)
	}
	if _, err := f.vault.GetSecret(ctx, 1, tracker.VaultItemID); !errors.Is(err, plugin.ErrSecretNotFound) {
		t.Fatalf("expected ErrSecretNotFound after shred, got %v", err)
	}
}

func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, 7, "demo", storedSecret{Login: "secret"})

	if _, err := f.plugin.findTracker(ctx, 2, 7, tracker.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-org lookup = %v", err)
	}
	if _, err := f.plugin.findTracker(ctx, 1, 8, tracker.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner lookup = %v", err)
	}
	if _, err := f.plugin.findTracker(ctx, 0, 7, tracker.ID); !errors.Is(err, errTenantUnavailable) {
		t.Fatalf("zero org lookup = %v", err)
	}

	rows, err := f.plugin.orgIDs(ctx)
	if err != nil {
		t.Fatalf("org ids: %v", err)
	}
	if len(rows) != 1 || rows[0] != 1 {
		t.Fatalf("org ids = %v", rows)
	}
}

func TestDeleteCascadeLeavesNoRows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, 7, "demo", storedSecret{Login: "secret"})
	if _, err := f.plugin.refresh(ctx, tracker, triggerManual); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := f.plugin.signin(ctx, tracker, triggerManual, false); err != nil {
		t.Fatalf("signin: %v", err)
	}

	if err := f.plugin.deleteTrackerCascade(ctx, 1, tracker.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, model := range []any{&PTTracker{}, &PTSnapshot{}, &PTSigninResult{}} {
		var count int64
		if err := f.db.Model(model).Count(&count).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 0 {
			t.Fatalf("%T still has %d rows", model, count)
		}
	}
}

func TestValidateRefusesWithoutVault(t *testing.T) {
	p := &Plugin{}
	if err := p.Validate(context.Background(), &fakeHost{}); err == nil {
		t.Fatal("a host without a vault must be refused")
	}
}

func TestCronSignsInDueTracker(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, 7, "demo", storedSecret{Login: "secret"})

	if err := f.plugin.runTick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	last, err := f.plugin.latestSignin(ctx, 1, tracker.ID)
	if err != nil {
		t.Fatalf("latest signin: %v", err)
	}
	if last.Trigger != triggerCron {
		t.Fatalf("trigger = %q", last.Trigger)
	}
	if last.Outcome != string(adapter.OutcomeAlreadySigned) {
		t.Fatalf("outcome = %q", last.Outcome)
	}

	reloaded, err := f.plugin.findTracker(ctx, 1, 7, tracker.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.NextSigninAt == nil || !reloaded.NextSigninAt.After(f.now) {
		t.Fatalf("next sign-in = %v", reloaded.NextSigninAt)
	}
}

func TestCronSkipsTrackersOutsideTheWindow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, 7, "demo", storedSecret{Login: "secret"})

	tracker.SigninWindowStartMin = 22 * 60
	tracker.SigninWindowEndMin = 23 * 60
	if err := f.plugin.saveTracker(ctx, tracker); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := f.plugin.runTick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := f.site.attendance.Load(); got != 0 {
		t.Fatalf("the site was contacted outside the window %d times", got)
	}
	reloaded, err := f.plugin.findTracker(ctx, 1, 7, tracker.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.NextSigninAt == nil {
		t.Fatal("no next sign-in was planned")
	}
}

func TestSigninUnsupportedRecordsNoSigninTime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, 7, "hddolby", storedSecret{APIKey: "key"})
	tracker.SchemaKey = string(adapter.SchemaHDDolby)
	if err := f.plugin.saveTracker(ctx, tracker); err != nil {
		t.Fatalf("save: %v", err)
	}

	before := f.site.hits.Load()
	res, err := f.plugin.signin(ctx, tracker, triggerManual, false)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != string(adapter.OutcomeUnsupported) {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	if got := f.site.hits.Load(); got != before {
		t.Fatalf("an unsupported sign-in contacted the site %d times", got-before)
	}

	reloaded, err := f.plugin.findTracker(ctx, 1, 7, tracker.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.LastSigninAt != nil {
		t.Fatalf("last sign-in = %v, want nil", reloaded.LastSigninAt)
	}
	if reloaded.LastSigninOutcome != string(adapter.OutcomeUnsupported) {
		t.Fatalf("outcome = %q", reloaded.LastSigninOutcome)
	}
	if reloaded.NextSigninAt == nil {
		t.Fatal("no next sign-in was planned")
	}
}

func TestFailedSigninKeepsLastSuccessTime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, 7, "demo", storedSecret{Login: "secret"})
	last := f.now.Add(-48 * time.Hour)
	f.plugin.patchTracker(ctx, tracker, map[string]any{"last_signin_at": last})

	f.site.attendanceBody = `<html><body><a href="/logout.php">Logout</a></body></html>`
	res, err := f.plugin.signin(ctx, tracker, triggerManual, false)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != string(adapter.OutcomeLoginOKUnconfirmed) {
		t.Fatalf("outcome = %q", res.Outcome)
	}

	reloaded, err := f.plugin.findTracker(ctx, 1, 7, tracker.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.LastSigninAt == nil || !reloaded.LastSigninAt.Equal(last) {
		t.Fatalf("last sign-in = %v, want %v", reloaded.LastSigninAt, last)
	}
}

func TestMarkAllDueSkipsUnsupportedSignin(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.createTracker(t, 1, 7, "nexus", storedSecret{Login: "secret"})
	unsupported := f.createTracker(t, 1, 7, "hddolby", storedSecret{APIKey: "key"})
	unsupported.SchemaKey = string(adapter.SchemaHDDolby)
	if err := f.plugin.saveTracker(ctx, unsupported); err != nil {
		t.Fatalf("save: %v", err)
	}

	queued, err := f.plugin.markAllDue(ctx, 1, 7, "signin")
	if err != nil {
		t.Fatalf("mark: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued = %d, want 1", queued)
	}
	reloaded, err := f.plugin.findTracker(ctx, 1, 7, unsupported.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.NextSigninAt != nil {
		t.Fatal("the unsupported site was marked due")
	}
}
