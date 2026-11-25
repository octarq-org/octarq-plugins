package pt

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/octarq-org/octarq/server/plugin"
)

type fakeCronService struct {
	started []string
	err     error
}

func (f *fakeCronService) Register(string, string, func(context.Context) error) error { return nil }
func (f *fakeCronService) Unregister(string) error                                    { return nil }
func (f *fakeCronService) List() []plugin.CronJob                                     { return nil }
func (f *fakeCronService) SetEnabled(string, bool) error                              { return nil }
func (f *fakeCronService) SetSpec(string, string) error                               { return nil }
func (f *fakeCronService) StartJob(name string) error {
	if f.err != nil {
		return f.err
	}
	f.started = append(f.started, name)
	return nil
}

func seedTracker(t *testing.T, r *routed, name string, enabled, signinEnabled, manual bool) *PTTracker {
	t.Helper()
	tracker := &PTTracker{
		OwnerUserID:        7,
		OrgID:              101,
		Name:               name,
		SecretID:           name + "-secret",
		BaseURL:            "https://example.invalid",
		Domain:             "example.invalid",
		SchemaKey:          "nexusphp",
		Enabled:            enabled,
		SigninEnabled:      signinEnabled,
		NeedsManualAction:  manual,
		StatsIntervalHours: 6,
		TimeoutSeconds:     20,
	}
	if err := r.db.Create(tracker).Error; err != nil {
		t.Fatalf("seed tracker: %v", err)
	}
	if err := r.db.Model(&PTTracker{}).Where("id = ?", tracker.ID).
		Updates(map[string]any{"enabled": enabled, "signin_enabled": signinEnabled, "needs_manual_action": manual}).Error; err != nil {
		t.Fatalf("seed tracker flags: %v", err)
	}
	return tracker
}

func TestRouteRunAllSignin(t *testing.T) {
	r := newRouted(t)
	cron := &fakeCronService{}
	r.plugin.ctx.Cron = cron

	ready := seedTracker(t, r, "ready", true, true, false)
	paused := seedTracker(t, r, "paused", false, true, false)
	manual := seedTracker(t, r, "manual", true, true, true)

	rec, body := r.do(t, http.MethodPost, "/api/pt/actions/run-all", "", map[string]any{"kind": "signin"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run-all -> %d %s", rec.Code, body)
	}
	var out struct {
		Started bool  `json:"started"`
		Queued  int64 `json:"queued"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if !out.Started || out.Queued != 1 {
		t.Fatalf("unexpected body: %s", body)
	}
	if len(cron.started) != 1 || cron.started[0] != cronTickJob {
		t.Fatalf("tick was not started: %+v", cron.started)
	}

	var reloaded PTTracker
	r.db.First(&reloaded, ready.ID)
	if reloaded.NextSigninAt == nil {
		t.Fatal("ready tracker was not marked due")
	}
	for _, untouched := range []*PTTracker{paused, manual} {
		var row PTTracker
		r.db.First(&row, untouched.ID)
		if row.NextSigninAt != nil {
			t.Fatalf("%s was marked due", untouched.Name)
		}
	}
}

func TestRouteRunAllRefreshKeepsSignin(t *testing.T) {
	r := newRouted(t)
	r.plugin.ctx.Cron = &fakeCronService{}
	tracker := seedTracker(t, r, "ready", true, true, false)

	rec, body := r.do(t, http.MethodPost, "/api/pt/actions/run-all", "", map[string]any{"kind": "refresh"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run-all -> %d %s", rec.Code, body)
	}
	var row PTTracker
	r.db.First(&row, tracker.ID)
	if row.NextStatsAt == nil {
		t.Fatal("stats were not marked due")
	}
	if row.NextSigninAt != nil {
		t.Fatal("sign-in was marked due by a refresh request")
	}
}

func TestRouteRunAllRejectsUnknownKind(t *testing.T) {
	r := newRouted(t)
	r.plugin.ctx.Cron = &fakeCronService{}

	rec, body := r.do(t, http.MethodPost, "/api/pt/actions/run-all", "", map[string]any{"kind": "delete"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown kind -> %d %s", rec.Code, body)
	}
}

func TestRouteRunAllBusyAndUnavailable(t *testing.T) {
	r := newRouted(t)
	seedTracker(t, r, "ready", true, true, false)

	r.plugin.ctx.Cron = &fakeCronService{err: plugin.ErrJobBusy}
	rec, body := r.do(t, http.MethodPost, "/api/pt/actions/run-all", "", map[string]any{"kind": "signin"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("busy -> %d %s", rec.Code, body)
	}

	r.plugin.ctx.Cron = nil
	rec, body = r.do(t, http.MethodPost, "/api/pt/actions/run-all", "", map[string]any{"kind": "signin"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no scheduler -> %d %s", rec.Code, body)
	}
	if strings.Contains(body, "pt:") {
		t.Fatalf("internal error leaked: %s", body)
	}
}
