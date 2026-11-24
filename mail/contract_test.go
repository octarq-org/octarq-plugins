package mail

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

func TestInboundEmailWireJSON(t *testing.T) {
	e := InboundEmail{
		ID:         7,
		MailboxID:  8,
		OrgID:      3,
		From:       "sender@example.com",
		To:         "user@example.net",
		Subject:    "Hello",
		Text:       "plain body",
		HTML:       "rich body",
		ReceivedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	got, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":7,"mailboxId":8,"orgId":3,"from":"sender@example.com","to":"user@example.net","subject":"Hello","text":"plain body","html":"rich body","receivedAt":"2026-01-02T03:04:05Z"}`
	if string(got) != want {
		t.Errorf("mail.inbound payload\n got: %s\nwant: %s", got, want)
	}
}

func TestWireMetricLiterals(t *testing.T) {
	if metricMailIn != "mailIn" {
		t.Errorf("metricMailIn = %q, want %q", metricMailIn, "mailIn")
	}
	if metricMailOut != "mailOut" {
		t.Errorf("metricMailOut = %q, want %q", metricMailOut, "mailOut")
	}
	if metricRawBytes != "mail.raw_bytes" {
		t.Errorf("metricRawBytes = %q, want %q", metricRawBytes, "mail.raw_bytes")
	}
	if ServiceStorageProvider != "storage.provider" {
		t.Errorf("ServiceStorageProvider = %q, want %q", ServiceStorageProvider, "storage.provider")
	}
}

func TestMountRegistersMessagingServices(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(testAllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	global := map[string]string{}
	settings := &plugin.TestSettings{
		GetGlobalSettingFn: func(key string) string { return global[key] },
		SetGlobalSettingFn: func(key, value string) error {
			global[key] = value
			return nil
		},
	}
	reg := plugin.NewRegistry()
	pctx := &plugin.Context{
		DB:      db,
		Provide: reg.Provide,
		Lookup:  reg.Lookup,
		Host:    &plugin.TestHost{SettingsMock: settings},
	}

	New().Mount(nil, pctx)

	for _, name := range []string{plugin.ServiceMessagingSend, plugin.ServiceMessagingSendSystem, plugin.ServiceMessagingReady} {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("Mount did not register %q", name)
		}
	}

	ready, ok := plugin.LookupAs[plugin.MessagingReady](pctx, plugin.ServiceMessagingReady)
	if !ok || ready == nil {
		t.Fatal("messaging readiness service does not resolve")
	}
	if ready() {
		t.Fatal("readiness must be false before a system sender is configured")
	}

	sender := SMTPSender{Name: "system", Host: "smtp.example.com", Port: 587, FromEmail: "noreply@example.com"}
	if err := db.Create(&sender).Error; err != nil {
		t.Fatalf("create system sender: %v", err)
	}
	global["mail_system_sender_id"] = fmt.Sprint(sender.ID)

	if !ready() {
		t.Fatal("readiness must be true after a system sender is configured")
	}
}
