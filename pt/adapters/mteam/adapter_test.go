package mteam

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
  "message": "SUCCESS",
  "data": {
    "id": 9001,
    "username": "alice",
    "role": 2,
    "memberStatus": {"createdDate": "2020-01-02 03:04:05"},
    "memberCount": {
      "uploaded": 1649267441664,
      "downloaded": 549755813888,
      "shareRate": 3.0,
      "bonus": 12345.6,
      "seeding": 12,
      "leeching": 1
    }
  }
}`

func newSession(t *testing.T, handler http.HandlerFunc, secret adapter.Secret) *adapter.Session {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	site := adapter.Site{
		BaseURL:    server.URL,
		APIBaseURL: server.URL,
		Schema:     adapter.SchemaMTeam,
		Timezone:   time.UTC,
		Timeout:    5 * time.Second,
		UserAgent:  "test",
	}
	client, err := adapter.NewClient(server.Client(), site, "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return &adapter.Session{Site: site, Secret: secret, Client: client, Now: time.Now()}
}

func TestStatsParsesProfile(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/member/profile" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "key-1" {
			t.Errorf("api key header = %q", r.Header.Get("x-api-key"))
		}
		_, _ = w.Write([]byte(profileJSON))
	}, adapter.Secret{APIKey: "key-1"})

	stats, err := New().Stats(context.Background(), s)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.UploadedBytes == nil || *stats.UploadedBytes != 1649267441664 {
		t.Fatalf("uploaded = %v", stats.UploadedBytes)
	}
	if stats.UserLevel != "Power User" {
		t.Fatalf("level = %q", stats.UserLevel)
	}
	if stats.Bonus == nil || *stats.Bonus != 12345.6 {
		t.Fatalf("bonus = %v", stats.Bonus)
	}
	if stats.Seeding == nil || *stats.Seeding != 12 {
		t.Fatalf("seeding = %v", stats.Seeding)
	}
}

func TestStatsRejectsUnauthorized(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}, adapter.Secret{APIKey: "key-1"})

	_, err := New().Stats(context.Background(), s)
	if !errors.Is(err, adapter.ErrAuth) {
		t.Fatalf("error = %v (%v)", adapter.KindOf(err), err)
	}
}

func TestSigninIsUnsupported(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("sign-in must not contact the site")
	}, adapter.Secret{APIKey: "key-1"})

	res, err := New().Signin(context.Background(), s)
	if err != nil {
		t.Fatalf("signin: %v", err)
	}
	if res.Outcome != adapter.OutcomeUnsupported {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	if New().Capabilities().Signin {
		t.Fatal("M-Team must not advertise a sign-in capability")
	}
}

func TestCheckSessionWithoutKeyIsConfigError(t *testing.T) {
	s := newSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("an empty key must not reach the site")
	}, adapter.Secret{})

	_, err := New().CheckSession(context.Background(), s)
	if !errors.Is(err, adapter.ErrConfig) {
		t.Fatalf("error = %v (%v)", adapter.KindOf(err), err)
	}
}
