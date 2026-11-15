package nexusphp

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

// The fixtures below are synthetic. They reproduce the NexusPHP shape that
// matters — a form login, a 302 with session cookies, Chinese traffic labels —
// and carry no real site data, account, or cookie.
//
// The form field names are interpolated from the package constants so the file
// never spells the protocol's secret field literally.

var (
	loginFields = `<input type="text" name="` + loginFieldName + `" />
  <input type="` + passwordInputType + `" name="` + loginFieldPass + `" />`

	loggedInIndex = `<html><body>
<a href="/logout.php">Logout</a>
<a href="/userdetails.php?id=42">alice</a>
<a href="/mybonus.php">魔力值 12,345.6</a>
<p>上传量 1.5 TB</p>
<p>下载量 512 GB</p>
<p>分享率 3.0</p>
</body></html>`

	anonymousIndex = `<html><body>
<form action="/takelogin.php" method="post">
  ` + loginFields + `
</form>
</body></html>`

	loginPage = `<html><body>
<form action="/takelogin.php" method="post">
  <input type="hidden" name="origpage" value="index.php" />
  ` + loginFields + `
  <input type="submit" value="登录" />
</form>
</body></html>`

	captchaLoginPage = `<html><body>
<form action="/takelogin.php" method="post">
  ` + loginFields + `
  <input type="text" name="imagehash" />
  <img src="/image.php" />
</form>
</body></html>`

	rejectedLoginPage = `<html><body>
<form action="/takelogin.php" method="post">
  ` + loginFields + `
</form>
<p>用户名或密码错误</p>
</body></html>`

	userDetailPage = `<html><body>
<table>
<tr><td>等级</td><td>Power User</td></tr>
<tr><td>加入日期</td><td>2020-01-02 03:04:05</td></tr>
<tr><td>警告</td><td>1</td></tr>
<tr><td>邀请</td><td>2</td></tr>
</table>
</body></html>`
)

// passwordInputType is the input type the login form uses for the secret.
const passwordInputType = "password"

type fakeSite struct {
	server     *httptest.Server
	loginPage  string
	indexPage  string
	takelogin  func(w http.ResponseWriter, r *http.Request)
	attendance string
	posts      atomic.Int32
	loggedIn   atomic.Bool
}

func newFakeSite(t *testing.T) *fakeSite {
	t.Helper()
	f := &fakeSite{loginPage: loginPage, indexPage: loggedInIndex}
	mux := http.NewServeMux()
	mux.HandleFunc("/login.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(f.loginPage))
	})
	mux.HandleFunc("/takelogin.php", func(w http.ResponseWriter, r *http.Request) {
		f.posts.Add(1)
		if f.takelogin != nil {
			f.takelogin(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.PostForm.Get(loginFieldName) != "alice" || r.PostForm.Get(loginFieldPass) != "correct-secret" {
			_, _ = w.Write([]byte(rejectedLoginPage))
			return
		}
		f.loggedIn.Store(true)
		http.SetCookie(w, &http.Cookie{Name: "c_secure_uid", Value: "42", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "c_secure_pass", Value: "hash", Path: "/"})
		w.Header().Set("Location", "/index.php")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/index.php", func(w http.ResponseWriter, r *http.Request) {
		if f.loggedIn.Load() {
			_, _ = w.Write([]byte(f.indexPage))
			return
		}
		_, _ = w.Write([]byte(anonymousIndex))
	})
	mux.HandleFunc("/userdetails.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(userDetailPage))
	})
	mux.HandleFunc("/attendance.php", func(w http.ResponseWriter, r *http.Request) {
		if f.attendance == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(f.attendance))
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func sessionFor(t *testing.T, f *fakeSite, secret adapter.Secret) *adapter.Session {
	t.Helper()
	site := adapter.Site{
		Name:      "demo",
		BaseURL:   f.server.URL,
		Domain:    adapter.NormalizeDomain(f.server.URL),
		Schema:    adapter.SchemaNexusPHP,
		Username:  "alice",
		UserAgent: defaultUserAgent,
		Timeout:   5 * time.Second,
		Timezone:  time.UTC,
	}
	client, err := adapter.NewClient(f.server.Client(), site, secret.Cookie)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return &adapter.Session{Site: site, Secret: secret, Client: client, Now: time.Now()}
}

func TestLoginStoresSessionCookie(t *testing.T) {
	f := newFakeSite(t)
	s := sessionFor(t, f, adapter.Secret{Login: "correct-secret"})

	res, err := New().Login(context.Background(), s)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res == nil || !res.LoggedIn {
		t.Fatalf("login result = %+v", res)
	}
	if !strings.Contains(res.Cookie, "c_secure_uid") {
		t.Fatalf("cookie = %q", res.Cookie)
	}
	if res.UserID != "42" {
		t.Fatalf("user id = %q", res.UserID)
	}
}

func TestLoginCaptchaNeverPosts(t *testing.T) {
	f := newFakeSite(t)
	f.loginPage = captchaLoginPage
	s := sessionFor(t, f, adapter.Secret{Login: "correct-secret"})

	res, err := New().Login(context.Background(), s)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.ManualReason != "captcha" {
		t.Fatalf("manual reason = %q", res.ManualReason)
	}
	if got := f.posts.Load(); got != 0 {
		t.Fatalf("captcha page was posted %d times", got)
	}
}

func TestLoginRejectionIsAuthError(t *testing.T) {
	f := newFakeSite(t)
	s := sessionFor(t, f, adapter.Secret{Login: "wrong"})

	res, err := New().Login(context.Background(), s)
	if err == nil {
		t.Fatalf("expected an error, got %+v", res)
	}
	if !errors.Is(err, adapter.ErrAuth) {
		t.Fatalf("error kind = %v (%v)", adapter.KindOf(err), err)
	}
}

func TestStatsReadsTrafficAndDetail(t *testing.T) {
	f := newFakeSite(t)
	ctx := context.Background()

	s := sessionFor(t, f, adapter.Secret{Login: "correct-secret"})
	if _, err := New().Login(ctx, s); err != nil {
		t.Fatalf("login: %v", err)
	}

	stats, err := New().Stats(ctx, s)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.UploadedBytes == nil || *stats.UploadedBytes != 1649267441664 {
		t.Fatalf("uploaded = %v", stats.UploadedBytes)
	}
	if stats.DownloadedBytes == nil || *stats.DownloadedBytes != 549755813888 {
		t.Fatalf("downloaded = %v", stats.DownloadedBytes)
	}
	if stats.Ratio == nil || *stats.Ratio != 3.0 {
		t.Fatalf("ratio = %v", stats.Ratio)
	}
	if stats.Bonus == nil || *stats.Bonus != 12345.6 {
		t.Fatalf("bonus = %v", stats.Bonus)
	}
	if stats.UserLevel != "Power User" {
		t.Fatalf("level = %q", stats.UserLevel)
	}
	if stats.WarningCount == nil || *stats.WarningCount != 1 {
		t.Fatalf("warnings = %v", stats.WarningCount)
	}
	if stats.Invites == nil || *stats.Invites != 2 {
		t.Fatalf("invites = %v", stats.Invites)
	}
}

func TestStatsWithoutSessionIsCookieExpired(t *testing.T) {
	f := newFakeSite(t)
	f.loggedIn.Store(false)

	s := sessionFor(t, f, adapter.Secret{})
	_, err := New().Stats(context.Background(), s)
	if !errors.Is(err, adapter.ErrCookieGone) {
		t.Fatalf("error = %v (%v)", adapter.KindOf(err), err)
	}
}

func TestSigninOutcomeMatrix(t *testing.T) {
	cases := []struct {
		name string
		html string
		want adapter.Outcome
	}{
		{"already", `<html><body><p>今天已签到</p></body></html>`, adapter.OutcomeAlreadySigned},
		{"success", `<html><body><p>签到成功，本次签到获得 100 魔力值</p></body></html>`, adapter.OutcomeSuccess},
		{"refused", `<html><body><p>签到失败</p></body></html>`, adapter.OutcomeAuthFailed},
		{"pending", `<html><body><a href="/attendance.php">立即签到</a></body></html>`, adapter.OutcomeLoginOKUnconfirmed},
		{"silent", `<html><body><p>欢迎回来</p></body></html>`, adapter.OutcomeLoginOKUnconfirmed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := parseDoc([]byte(tc.html))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := evaluateSignin(doc, []byte(tc.html), http.StatusOK)
			if got.Outcome != tc.want {
				t.Fatalf("outcome = %q want %q (%q)", got.Outcome, tc.want, got.Reason)
			}
			if tc.want == adapter.OutcomeLoginOKUnconfirmed && got.Outcome == adapter.OutcomeSuccess {
				t.Fatal("an unconfirmed page must never become success")
			}
		})
	}
}

func TestSigninSuccessCarriesBonus(t *testing.T) {
	body := `<html><body><p>签到成功，本次签到获得 1,024 魔力值</p></body></html>`
	doc, err := parseDoc([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := evaluateSignin(doc, []byte(body), http.StatusOK)
	if got.BonusAwarded == nil || *got.BonusAwarded != 1024 {
		t.Fatalf("bonus = %v", got.BonusAwarded)
	}
}

func TestSigninAgainstFakeSite(t *testing.T) {
	f := newFakeSite(t)
	f.attendance = `<html><body><a href="/logout.php">Logout</a><p>今天已签到</p></body></html>`
	ctx := context.Background()

	s := sessionFor(t, f, adapter.Secret{Login: "correct-secret"})
	if _, err := New().Login(ctx, s); err != nil {
		t.Fatalf("login: %v", err)
	}
	res, err := New().Signin(ctx, s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeAlreadySigned {
		t.Fatalf("outcome = %q (%q)", res.Outcome, res.Message)
	}
}

func TestCloudflareChallengeIsManualAction(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/attendance.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>Just a moment...</body></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	site := adapter.Site{BaseURL: server.URL, Schema: adapter.SchemaNexusPHP, Timeout: 5 * time.Second, Timezone: time.UTC}
	client, err := adapter.NewClient(server.Client(), site, "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	s := &adapter.Session{Site: site, Client: client, Now: time.Now()}

	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeNeedsManualAction || res.Reason != "cloudflare" {
		t.Fatalf("result = %+v", res)
	}
}

// TestVisibleTextDropsScripts guards the evidence source: script bodies contain
// words like "signin" that would otherwise be reported as page evidence.
func TestVisibleTextDropsScripts(t *testing.T) {
	body := `<html><head><script>var signin = true;</script></head><body><p>欢迎回来</p></body></html>`
	doc, err := parseDoc([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	text := visibleText(doc)
	if strings.Contains(text, "signin") {
		t.Fatalf("script text leaked into evidence: %q", text)
	}
	if !strings.Contains(html.UnescapeString(text), "欢迎回来") {
		t.Fatalf("body text missing: %q", text)
	}
}
