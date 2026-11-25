package nexusphp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

type variantSite struct {
	indexPage        string
	addbonusPage     string
	attendanceStatus int

	mu    sync.Mutex
	paths []string
}

func (v *variantSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	v.mu.Lock()
	v.paths = append(v.paths, r.URL.RequestURI())
	v.mu.Unlock()

	switch r.URL.Path {
	case "/attendance.php":
		w.WriteHeader(v.attendanceStatus)
		return
	case "/index.php":
		if r.URL.Query().Get("action") == "addbonus" {
			if v.addbonusPage != "" {
				_, _ = io.WriteString(w, v.addbonusPage)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, v.indexPage)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (v *variantSite) requested(uri string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	n := 0
	for _, p := range v.paths {
		if p == uri {
			n++
		}
	}
	return n
}

func (v *variantSite) count() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.paths)
}

func variantSession(t *testing.T, v *variantSite) *adapter.Session {
	t.Helper()
	server := httptest.NewServer(v)
	t.Cleanup(server.Close)
	site := adapter.Site{
		Name:      "demo",
		BaseURL:   server.URL,
		Domain:    adapter.NormalizeDomain(server.URL),
		Schema:    adapter.SchemaNexusPHP,
		Username:  "alice",
		UserAgent: defaultUserAgent,
		Timeout:   5 * time.Second,
		Timezone:  time.UTC,
	}
	client, err := adapter.NewClient(server.Client(), site, "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return &adapter.Session{Site: site, Client: client, Now: time.Now()}
}

func TestSigninFollowsAddbonusLinkWhenAttendanceMissing(t *testing.T) {
	v := &variantSite{
		attendanceStatus: http.StatusNotFound,
		indexPage: `<html><body>
<a href="/logout.php">Logout</a>
<a href="index.php?action=addbonus">每日签到</a>
</body></html>`,
		addbonusPage: `<html><body><a href="/logout.php">Logout</a><p>签到成功，本次签到获得 128 魔力值</p></body></html>`,
	}
	s := variantSession(t, v)

	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeSuccess {
		t.Fatalf("outcome = %q (%q)", res.Outcome, res.Message)
	}
	if res.Reason != "confirmed" {
		t.Fatalf("reason = %q", res.Reason)
	}
	if res.BonusAwarded == nil || *res.BonusAwarded != 128 {
		t.Fatalf("bonus = %v", res.BonusAwarded)
	}
	if got := v.requested("/index.php?action=addbonus"); got != 1 {
		t.Fatalf("addbonus requested %d times", got)
	}
}

func TestSigninWithoutEntryPointIsUnsupported(t *testing.T) {
	v := &variantSite{
		attendanceStatus: http.StatusNotFound,
		indexPage: `<html><body>
<a href="/logout.php">Logout</a>
<a href="/userdetails.php?id=42">alice</a>
<p>欢迎回来</p>
</body></html>`,
	}
	s := variantSession(t, v)

	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeUnsupported {
		t.Fatalf("outcome = %q (%q)", res.Outcome, res.Message)
	}
	if res.Reason != "no_signin_endpoint" {
		t.Fatalf("reason = %q", res.Reason)
	}
	if res.Message != "This site has no daily sign-in." {
		t.Fatalf("message = %q", res.Message)
	}
	if got := v.count(); got != 2 {
		t.Fatalf("requests = %d (%v), want attendance plus one index", got, v.paths)
	}
}

func TestSigninIndexLoginFormIsCookieExpired(t *testing.T) {
	v := &variantSite{
		attendanceStatus: http.StatusNotFound,
		indexPage:        anonymousIndex,
	}
	s := variantSession(t, v)

	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeAuthFailed || res.Reason != "cookie_expired" {
		t.Fatalf("result = %+v", res)
	}
}

func TestSigninIndexAlreadySigned(t *testing.T) {
	v := &variantSite{
		attendanceStatus: http.StatusNotFound,
		indexPage: `<html><body>
<a href="/logout.php">Logout</a>
<p>今天已签到</p>
</body></html>`,
	}
	s := variantSession(t, v)

	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeAlreadySigned || res.Reason != "already_signed" {
		t.Fatalf("result = %+v", res)
	}
	if got := v.count(); got != 2 {
		t.Fatalf("requests = %d (%v), want attendance plus one index", got, v.paths)
	}
}

func TestSigninAttendanceServerErrorIsNetworkError(t *testing.T) {
	v := &variantSite{
		attendanceStatus: http.StatusInternalServerError,
		indexPage:        `<html></html>`,
	}
	s := variantSession(t, v)

	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeNetworkError || res.Reason != "http_status" {
		t.Fatalf("result = %+v", res)
	}
	if res.HTTPStatus != http.StatusInternalServerError {
		t.Fatalf("status = %d", res.HTTPStatus)
	}
	if got := v.requested("/index.php"); got != 0 {
		t.Fatalf("index requested %d times after a server error", got)
	}
}
