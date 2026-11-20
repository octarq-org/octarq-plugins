package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

func TestNotifyReceivesStoredConfigVerbatim(t *testing.T) {
	const stored = "v1:AAAABBBBCCCC=="

	var gotType, gotConfig, gotText string
	calls := 0

	p := New()
	p.Mount(nil, &plugin.Context{
		Notify: func(_ context.Context, kind, cfgJSON, message string) error {
			calls++
			gotType, gotConfig, gotText = kind, cfgJSON, message
			return nil
		},
	})

	if p.notify == nil {
		t.Fatal("Mount did not wire the notify hook")
		return
	}
	if err := p.notify(context.Background(), "telegram", stored, "hello"); err != nil {
		t.Fatalf("notify: %v", err)
	}

	if calls != 1 {
		t.Fatalf("notify called %d times, want 1", calls)
	}
	if gotConfig != stored {
		t.Errorf("config was altered in transit:\n got  %q\n want %q\nA plugin that parses the stored config cannot handle an encrypted one.", gotConfig, stored)
	}
	if gotType != "telegram" || gotText != "hello" {
		t.Errorf("type/text mangled: type=%q text=%q", gotType, gotText)
	}
}

func TestEncryptedConfigReachesProvider(t *testing.T) {
	plain := `{"botToken":"secret-token","chatId":"42"}`
	const sealed = "sealed:" + `{"botToken":"secret-token","chatId":"42"}`

	decryptor := func(stored string) (string, bool) {
		if len(stored) > 7 && stored[:7] == "sealed:" {
			return stored[7:], true
		}
		return "", false
	}

	var delivered string
	testSend := func(_ context.Context, typ, cfgJSON, _ string) error {
		if typ == "test-provider" {
			if pt, ok := decryptor(cfgJSON); ok {
				delivered = pt
			}
		}
		return nil
	}

	p := New()
	p.Mount(nil, &plugin.Context{Notify: testSend})

	if err := p.notify(context.Background(), "test-provider", sealed, "body"); err != nil {
		t.Fatalf("notify: %v", err)
	}

	if delivered != plain {
		t.Fatalf("provider got %q, want the decrypted config %q", delivered, plain)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(delivered), &cfg); err != nil {
		t.Fatalf("provider got something that is not JSON (%q): %v", delivered, err)
	}
	if cfg["botToken"] != "secret-token" {
		t.Errorf("botToken = %v, want secret-token", cfg["botToken"])
	}
}

func TestInboundWebhookForwardsStoredConfig(t *testing.T) {
	const sealedConfig = "sealed:{\"botToken\":\"t\",\"chatId\":\"1\"}"

	db, err := gorm.Open(sqlite.Open("file:mailnotify?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(append(testAllModels(), &Mailbox{}, &Email{}, &SMTPSender{})...); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const orgID uint = 5
	org := testOrg{Slug: "acme", InboundToken: "tok-inbound"}
	org.ID = orgID
	if err := db.Create(&org).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := db.Create(&Mailbox{OrgID: orgID, Address: "hi@acme.test", Enabled: true}).Error; err != nil {
		t.Fatalf("seed mailbox: %v", err)
	}
	if err := db.Create(&testNotificationChannel{
		OrgID: orgID, Name: "tg", Type: "telegram", Config: sealedConfig, Enabled: true,
	}).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	got := make(chan string, 4)
	p := New()
	p.Mount(nil, &plugin.Context{
		DB: db,
		Notify: func(_ context.Context, _, cfgJSON, _ string) error {
			got <- cfgJSON
			return nil
		},
	})

	raw := "From: a@b.test\r\nTo: hi@acme.test\r\nSubject: hello\r\n\r\nbody\r\n"
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/acme/email/inbound/tok-inbound", strings.NewReader(raw))
	req.Header.Set("X-Octarq-To", "hi@acme.test")
	rec := httptest.NewRecorder()

	if _, err := p.inbound(context.Background(), &InboundInput{
		Ctx:     humago.NewContext(nil, req, rec),
		OrgSlug: "acme",
		Token:   "tok-inbound",
	}); err != nil {
		t.Fatalf("inbound: %v", err)
	}

	select {
	case cfg := <-got:
		if cfg != sealedConfig {
			t.Errorf("notification sent with %q, want the stored config %q — the webhook must not transform it", cfg, sealedConfig)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no notification dispatched for the inbound email")
	}
}

func assertNotifyHookShape(func(ctx context.Context, kind, cfgJSON, message string) error) {}

var _ = func(p *Plugin) { assertNotifyHookShape(p.notify) }
