package mail

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

func captureSMTP(t *testing.T) (host string, port int, got func() string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	var mu sync.Mutex
	var captured string
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		w.WriteString("220 fake ESMTP\r\n")
		w.Flush()
		inData := false
		var body strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					inData = false
					mu.Lock()
					captured = body.String()
					mu.Unlock()
					w.WriteString("250 ok\r\n")
					w.Flush()
					continue
				}
				body.WriteString(line + "\n")
				continue
			}
			verb := strings.ToUpper(strings.Fields(line)[0])
			switch verb {
			case "EHLO":
				w.WriteString("250-fake greets you\r\n250-AUTH PLAIN\r\n250 SIZE 1000000\r\n")
			case "AUTH":
				w.WriteString("235 ok\r\n")
			case "MAIL", "RCPT":
				w.WriteString("250 ok\r\n")
			case "DATA":
				w.WriteString("354 go ahead\r\n")
				inData = true
			case "QUIT":
				w.WriteString("221 bye\r\n")
				w.Flush()
				return
			default:
				w.WriteString("250 ok\r\n")
			}
			w.Flush()
		}
	}()
	host = "127.0.0.1"
	port = ln.Addr().(*net.TCPAddr).Port
	return host, port, func() string {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		mu.Lock()
		defer mu.Unlock()
		return captured
	}
}

func newSystemMailTestPlugin(t *testing.T, globalSetting func(string) string) (*Plugin, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&SMTPSender{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Where("1 = 1").Delete(&SMTPSender{})
	p := New()
	p.db = db
	p.vaultSvc = newMockVaultService()
	if globalSetting != nil {
		p.getGlobalSetting = globalSetting
	}
	return p, db
}

func TestSendSystemMailNoSenderReturnsClearError(t *testing.T) {
	p, _ := newSystemMailTestPlugin(t, nil)

	if p.mailReady() {
		t.Fatal("mailReady = true with no sender configured")
	}
	err := p.sendSystemMail("user@example.com", "Verify your octarq email", "", "verify me")
	if err == nil {
		t.Fatal("sendSystemMail succeeded with no SMTP sender configured")
		return
	}
	if !strings.Contains(err.Error(), "no SMTP sender") {
		t.Fatalf("sendSystemMail error does not explain the missing sender: %v", err)
	}
}

func TestSendSystemMailDeliversWithoutOrg(t *testing.T) {
	host, port, got := captureSMTP(t)
	p, _ := newSystemMailTestPlugin(t, func(key string) string {
		if key == "mail_system_sender_id" {
			return "1"
		}
		return ""
	})
	if _, err := seedVaultSMTPSender(p, SMTPSender{
		ID: 1, OrgID: 1, Name: "relay", Host: host, Port: port,
		User: "u", FromEmail: "noreply@example.com",
	}, "pw"); err != nil {
		t.Fatalf("seed sender: %v", err)
	}

	err := p.sendSystemMail("user@example.com", "Verify your octarq email", "", "verify me")
	if err != nil {
		t.Fatalf("sendSystemMail failed: %v", err)
	}
	body := got()
	if !strings.Contains(body, "verify me") {
		t.Fatalf("relay received no expected payload:\n%s", body)
	}
}

func TestSystemSenderResolution(t *testing.T) {
	settings := map[string]string{}
	p, db := newSystemMailTestPlugin(t, func(key string) string { return settings[key] })
	first := SMTPSender{ID: 1, OrgID: 1, Name: "first"}
	second := SMTPSender{ID: 2, OrgID: 2, Name: "second"}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("seed first: %v", err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatalf("seed second: %v", err)
	}

	if _, err := p.systemSender(); err == nil {
		t.Fatal("unset setting must fail closed without fallback to lowest id")
	}

	settings["mail_system_sender_id"] = "2"
	s, err := p.systemSender()
	if err != nil {
		t.Fatalf("systemSender: %v", err)
	}
	if s.ID != 2 {
		t.Fatalf("configured setting resolved sender %d, want 2", s.ID)
	}

	settings["mail_system_sender_id"] = "999"
	if _, err := p.systemSender(); err == nil {
		t.Fatal("stale setting must fail closed without fallback to lowest id")
	}
}

func TestVaultService_MailCredentialResolution(t *testing.T) {
	p, _ := newSystemMailTestPlugin(t, func(string) string { return "" })

	if p.mailReady() {
		t.Fatal("expected mailReady = false initially")
	}

	host, port, got := captureSMTP(t)
	mv := p.vaultSvc.(*mockVaultService)

	if _, err := mv.PutSecret(context.Background(), plugin.PutSecretRequest{
		OrgID:        1,
		Scope:        plugin.VaultScopeOrg,
		ItemType:     "integration",
		ProviderType: "communication",
		Provider:     "smtp",
		Name:         "Centralized SMTP",
		Payload: map[string]any{
			"host":      host,
			"port":      port,
			"fromEmail": "pro-sender@octarq.com",
		},
	}); err != nil {
		t.Fatal(err)
	}

	if p.mailReady() {
		t.Fatal("expected mailReady = false while only an org-scoped integration exists")
	}

	if err := p.sendMail(1, "recipient@example.com", "Test Subject Via Vault", "<p>Body</p>", "Body"); err != nil {
		t.Fatalf("sendMail via VaultService failed: %v", err)
	}

	captured := got()
	if !strings.Contains(captured, "Test Subject Via Vault") {
		t.Fatalf("SMTP relay did not receive expected subject: %s", captured)
	}
	if !strings.Contains(captured, "pro-sender@octarq.com") {
		t.Fatalf("SMTP relay did not receive expected from address: %s", captured)
	}

	hostInst, portInst, gotInst := captureSMTP(t)
	instMeta, err := mv.PutInstanceSecret(context.Background(), plugin.PutSecretRequest{
		Scope:        plugin.VaultScopeInstance,
		ItemType:     "integration",
		ProviderType: "communication",
		Provider:     "smtp",
		KeyName:      "mail.system.default",
		Name:         "Instance System SMTP",
		Payload: map[string]any{
			"smtpHost":  hostInst,
			"smtpPort":  portInst,
			"apiKey":    "sys-user",
			"secretKey": "sys-pass",
			"fromEmail": "system@octarq.com",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	p.getGlobalSetting = func(key string) string {
		if key == "mail_system_sender_id" {
			return fmt.Sprintf("%d", instMeta.ID)
		}
		return ""
	}
	if !p.mailReady() {
		t.Fatal("expected mailReady = true with instance-scoped system sender")
	}
	if err := p.sendSystemMail("user@example.com", "Instance System Subject", "", "hello"); err != nil {
		t.Fatalf("sendSystemMail via instance vault failed: %v", err)
	}
	if !strings.Contains(gotInst(), "system@octarq.com") {
		t.Fatalf("instance relay did not receive expected from address: %s", gotInst())
	}

	resendSec := &plugin.VaultSecret{
		Meta:    plugin.VaultItemMeta{ID: 102, OrgID: 1, ProviderType: "communication", Provider: "resend"},
		Payload: map[string]any{"apiKey": "re_123456", "fromEmail": "resend@octarq.com"},
	}
	sender, from, err := p.senderFromVaultSecret(resendSec)
	if err != nil || sender == nil || from != "resend@octarq.com" {
		t.Fatalf("senderFromVaultSecret for resend failed: %v, from=%s", err, from)
	}
}
