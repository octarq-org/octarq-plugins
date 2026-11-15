package rousi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

const profileJSON = `{
  "code": 0,
  "message": "ok",
  "data": {
    "username": "alice",
    "level_text": "Power User",
    "uploaded": 1649267441664,
    "downloaded": 0,
    "ratio": 0,
    "karma": 987.5,
    "registered_at": "2020-01-02T03:04:05Z",
    "seeding_leeching_data": {
      "seeding_count": 9,
      "seeding_size": 1099511627776,
      "leeching_count": 0,
      "leeching_size": 0
    }
  }
}`

func newSession(t *testing.T, handler http.HandlerFunc) *adapter.Session {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	site := adapter.Site{
		BaseURL:   server.URL,
		Schema:    adapter.SchemaRousi,
		Timezone:  time.UTC,
		Timeout:   5 * time.Second,
		UserAgent: "test",
	}
	client, err := adapter.NewClient(server.Client(), site, "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return &adapter.Session{Site: site, Secret: adapter.Secret{APIKey: "key-1"}, Client: client, Now: time.Now()}
}

func TestStatsParsesProfile(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/profile" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer key-1" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(profileJSON))
	})

	stats, err := New().Stats(context.Background(), s)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Username != "alice" || stats.UserLevel != "Power User" {
		t.Fatalf("identity = %q/%q", stats.Username, stats.UserLevel)
	}
	if stats.Bonus == nil || *stats.Bonus != 987.5 {
		t.Fatalf("bonus = %v", stats.Bonus)
	}
	if stats.Seeding == nil || *stats.Seeding != 9 {
		t.Fatalf("seeding = %v", stats.Seeding)
	}
	if stats.DownloadedBytes == nil || *stats.DownloadedBytes != 0 {
		t.Fatalf("download zero must stay a real zero: %v", stats.DownloadedBytes)
	}
}

func TestNonZeroCodeIsAuthError(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code": 401, "message": "bad token"}`))
	})
	_, err := New().Stats(context.Background(), s)
	if !errors.Is(err, adapter.ErrAuth) {
		t.Fatalf("error = %v (%v)", adapter.KindOf(err), err)
	}
}

func TestSigninSuccessRequiresZeroCode(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/points/attendance" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code": 0, "message": "签到成功"}`))
	})
	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeSuccess {
		t.Fatalf("outcome = %q", res.Outcome)
	}
}

func TestSigninAlreadySigned(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code": 1, "message": "今天已签到"}`))
	})
	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeAlreadySigned {
		t.Fatalf("outcome = %q", res.Outcome)
	}
}

func TestSigninRefusalStaysFailure(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code": 500, "message": "服务器错误"}`))
	})
	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome == adapter.OutcomeSuccess {
		t.Fatalf("a non-zero code became success: %+v", res)
	}
}
