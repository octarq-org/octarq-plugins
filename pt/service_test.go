package pt

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

// ---------- fake credential vault ----------

// fakeVault is an in-memory CryptoVault. A subject key is derived from the
// subject name and removed by ShredSubjectKey, so a shredded subject fails
// closed exactly like the real vault.
type fakeVault struct {
	keys map[string][]byte
}

func newFakeVault() *fakeVault { return &fakeVault{keys: map[string][]byte{}} }

func subjectKey(owner uint, subject string) string {
	return fmt.Sprintf("%d/%s", owner, subject)
}

func deriveKey(seed string) []byte {
	sum := sha256.Sum256([]byte(seed))
	return sum[:]
}

func (v *fakeVault) seal(key, plaintext, aad []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, plaintext, aad)
	return base64.StdEncoding.EncodeToString(out), nil
}

func (v *fakeVault) open(key []byte, encoded string, aad []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, body := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, body, aad)
}

func (v *fakeVault) Encrypt(plaintext []byte) (string, error) {
	return v.seal(deriveKey("instance"), plaintext, nil)
}

func (v *fakeVault) Decrypt(encoded string) ([]byte, error) {
	return v.open(deriveKey("instance"), encoded, nil)
}

func (v *fakeVault) EncryptWithAAD(plaintext, aad []byte) (string, error) {
	return v.seal(deriveKey("instance"), plaintext, aad)
}

func (v *fakeVault) DecryptWithAAD(encoded string, aad []byte) ([]byte, error) {
	return v.open(deriveKey("instance"), encoded, aad)
}

func (v *fakeVault) SubjectVault(ownerID uint, subject string) (plugin.CryptoVault, error) {
	key, ok := v.keys[subjectKey(ownerID, subject)]
	if !ok {
		return nil, errors.New("subject key is not available")
	}
	return &subjectVault{vault: v, key: key}, nil
}

func (v *fakeVault) ShredSubjectKey(ownerID uint, subject string) error {
	delete(v.keys, subjectKey(ownerID, subject))
	return nil
}

type subjectVault struct {
	vault *fakeVault
	key   []byte
}

func (s *subjectVault) Encrypt(plaintext []byte) (string, error) {
	return s.vault.seal(s.key, plaintext, nil)
}

func (s *subjectVault) Decrypt(encoded string) ([]byte, error) {
	return s.vault.open(s.key, encoded, nil)
}

func (s *subjectVault) EncryptWithAAD(plaintext, aad []byte) (string, error) {
	return s.vault.seal(s.key, plaintext, aad)
}

func (s *subjectVault) DecryptWithAAD(encoded string, aad []byte) ([]byte, error) {
	return s.vault.open(s.key, encoded, aad)
}

func (s *subjectVault) SubjectVault(uint, string) (plugin.CryptoVault, error) {
	return nil, errors.New("no nested vaults")
}
func (s *subjectVault) ShredSubjectKey(uint, string) error { return nil }

// ---------- fake host ----------

type fakeSession struct{}

func (fakeSession) UserID(*http.Request) uint                      { return 7 }
func (fakeSession) OrgID(*http.Request) uint                       { return 1 }
func (fakeSession) OrgRole(*http.Request) string                   { return "admin" }
func (fakeSession) RequireRole(*http.Request, string) bool         { return true }
func (fakeSession) RequirePerm(*http.Request, string, string) bool { return true }
func (fakeSession) IsInstanceAdmin(*http.Request) bool             { return true }
func (fakeSession) RevokeUserOrgSessions(uint, uint) int           { return 0 }

// fakeHost embeds the interface so only the methods a test needs are real.
type fakeHost struct {
	plugin.Host
	db     *gorm.DB
	crypto plugin.CryptoVault
}

func (h *fakeHost) Session() plugin.HostSession { return fakeSession{} }
func (h *fakeHost) Crypto() plugin.CryptoVault  { return h.crypto }

func (h *fakeHost) TenantDB(orgID uint) *plugin.TenantDB {
	tdb, err := plugin.NewTenantDB(h.db, orgID)
	if err != nil {
		return nil
	}
	return tdb
}

// ---------- fake NexusPHP site ----------

type fakeSite struct {
	server         *httptest.Server
	attendance     atomic.Int32
	logins         atomic.Int32
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

	// A session is a cookie the login endpoint issued, not any cookie at all.
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

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// ---------- fixture ----------

type fixture struct {
	plugin *Plugin
	db     *gorm.DB
	vault  *fakeVault
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
	if err := db.AutoMigrate(&PTTracker{}, &PTCredential{}, &PTSnapshot{}, &PTSigninResult{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	vault := newFakeVault()
	for _, orgID := range []uint{1, 2} {
		vault.keys[subjectKey(orgID, credentialSubject("seed"))] = deriveKey("seed")
	}

	site := newFakeSite(t)
	host := &fakeHost{db: db, crypto: vault}
	f := &fixture{db: db, vault: vault, site: site, now: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)}

	f.plugin = &Plugin{
		db:   db,
		host: host,
		newClient: func(time.Duration) *http.Client {
			return site.server.Client()
		},
		now: func() time.Time { return f.now },
	}
	return f
}

// createTracker inserts a tracker row and its encrypted credential.
func (f *fixture) createTracker(t *testing.T, orgID uint, name string, secret storedSecret) *PTTracker {
	t.Helper()
	ctx := context.Background()

	secretID, err := newSecretID()
	if err != nil {
		t.Fatalf("secret id: %v", err)
	}
	f.vault.keys[subjectKey(orgID, credentialSubject(secretID))] = deriveKey(secretID)

	tracker := &PTTracker{
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
		CreatedByUserID:      7,
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

// ---------- tests ----------

func TestLoginStoresEncryptedCookie(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, "demo", storedSecret{Login: "secret"})

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

	cred, err := f.plugin.credentialFor(ctx, 1, tracker.ID)
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	if cred.Ciphertext == "" {
		t.Fatal("no ciphertext stored")
	}
	if strings.Contains(cred.Ciphertext, "c_secure_uid") {
		t.Fatal("ciphertext contains a cookie name in plaintext")
	}
	if cred.SecretVersion != 2 {
		t.Fatalf("secret version = %d", cred.SecretVersion)
	}
	if !cred.HasCookie {
		t.Fatal("has_cookie flag not set")
	}

	secret, err := f.plugin.decryptSecret(1, tracker.SecretID, cred.Ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
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
	tracker := f.createTracker(t, 1, "demo", storedSecret{Login: "secret"})

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
	tracker := f.createTracker(t, 1, "demo", storedSecret{Login: "secret"})

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

	// force bypasses the ledger on purpose, once the domain gap has passed.
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
	first := f.createTracker(t, 1, "first", storedSecret{Login: "secret"})
	second := f.createTracker(t, 1, "second", storedSecret{Login: "other"})

	cred, err := f.plugin.credentialFor(ctx, 1, first.ID)
	if err != nil {
		t.Fatalf("credential: %v", err)
	}

	// Move the ciphertext to the other tracker: the AAD no longer matches.
	if _, err := f.plugin.decryptSecret(1, second.SecretID, cred.Ciphertext); err == nil {
		t.Fatal("ciphertext decrypted under another tracker")
	}
	// Same tracker in another workspace: the AAD and the subject key differ.
	if _, err := f.plugin.decryptSecret(2, first.SecretID, cred.Ciphertext); err == nil {
		t.Fatal("ciphertext decrypted in another workspace")
	}
}

func TestShredMakesCiphertextUnreadable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, "demo", storedSecret{Login: "secret"})

	cred, err := f.plugin.credentialFor(ctx, 1, tracker.ID)
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	if err := f.plugin.shredSecret(1, tracker.SecretID); err != nil {
		t.Fatalf("shred: %v", err)
	}
	if _, err := f.plugin.decryptSecret(1, tracker.SecretID, cred.Ciphertext); err == nil {
		t.Fatal("ciphertext decrypted after the key was destroyed")
	}
}

func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, "demo", storedSecret{Login: "secret"})

	if _, err := f.plugin.findTracker(ctx, 2, tracker.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace lookup = %v", err)
	}
	if _, err := f.plugin.findTracker(ctx, 0, tracker.ID); !errors.Is(err, errTenantUnavailable) {
		t.Fatalf("zero workspace lookup = %v", err)
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
	tracker := f.createTracker(t, 1, "demo", storedSecret{Login: "secret"})
	if _, err := f.plugin.refresh(ctx, tracker, triggerManual); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := f.plugin.signin(ctx, tracker, triggerManual, false); err != nil {
		t.Fatalf("signin: %v", err)
	}

	if err := f.plugin.deleteTrackerCascade(ctx, 1, tracker.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, model := range []any{&PTTracker{}, &PTCredential{}, &PTSnapshot{}, &PTSigninResult{}} {
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

func TestHasEncryptedData(t *testing.T) {
	f := newFixture(t)
	has, err := f.plugin.HasEncryptedData()
	if err != nil {
		t.Fatalf("has encrypted data: %v", err)
	}
	if has {
		t.Fatal("an empty database reported encrypted data")
	}

	f.createTracker(t, 1, "demo", storedSecret{Login: "secret"})
	has, err = f.plugin.HasEncryptedData()
	if err != nil {
		t.Fatalf("has encrypted data: %v", err)
	}
	if !has {
		t.Fatal("a stored credential was not reported")
	}
}

func TestCronSignsInDueTracker(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tracker := f.createTracker(t, 1, "demo", storedSecret{Login: "secret"})

	// A tracker with no planned run is due immediately.
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

	reloaded, err := f.plugin.findTracker(ctx, 1, tracker.ID)
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
	tracker := f.createTracker(t, 1, "demo", storedSecret{Login: "secret"})

	// Window 22:00–23:00 UTC; the fixture clock is 09:00 UTC.
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
	reloaded, err := f.plugin.findTracker(ctx, 1, tracker.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.NextSigninAt == nil {
		t.Fatal("no next sign-in was planned")
	}
}
