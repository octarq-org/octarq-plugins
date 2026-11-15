package hddolby

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

const userDataJSON = `{
  "status": 0,
  "msg": "ok",
  "data": [{
    "uploaded": 1649267441664,
    "downloaded": 549755813888,
    "seedbonus": 4321.5,
    "unread_messages": 2,
    "class": 7,
    "added": "2020-01-02 03:04:05"
  }]
}`

func newSession(t *testing.T, handler http.HandlerFunc) *adapter.Session {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	site := adapter.Site{
		BaseURL:   server.URL,
		Schema:    adapter.SchemaHDDolby,
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

func TestStatsParsesUserData(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user/data" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "key-1" {
			t.Errorf("api key header = %q", r.Header.Get("x-api-key"))
		}
		_, _ = w.Write([]byte(userDataJSON))
	})

	stats, err := New().Stats(context.Background(), s)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Bonus == nil || *stats.Bonus != 4321.5 {
		t.Fatalf("bonus = %v", stats.Bonus)
	}
	if stats.UserLevel != "Class 7" {
		t.Fatalf("level = %q", stats.UserLevel)
	}
	if stats.UnreadMessages == nil || *stats.UnreadMessages != 2 {
		t.Fatalf("unread = %v", stats.UnreadMessages)
	}
	if stats.JoinAt == nil {
		t.Fatal("join date missing")
	}
}

func TestNonZeroStatusIsAuthError(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status": 1, "msg": "bad key", "data": []}`))
	})

	_, err := New().Stats(context.Background(), s)
	if !errors.Is(err, adapter.ErrAuth) {
		t.Fatalf("error = %v (%v)", adapter.KindOf(err), err)
	}
}

func TestSigninIsUnsupported(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("sign-in must not contact the site")
	})
	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeUnsupported {
		t.Fatalf("outcome = %q", res.Outcome)
	}
}
