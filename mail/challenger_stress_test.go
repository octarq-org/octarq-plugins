package mail

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	internalmail "github.com/octarq-org/octarq-plugins/mail/pkg/mail"
	"github.com/octarq-org/octarq/server/plugin"
)

// TestChallenger_MailCrossTenantIsolation stress tests cross-tenant boundaries
// in sendEmail (HTTP API) and sendMail (internal service).
func TestChallenger_MailCrossTenantIsolation(t *testing.T) {
	p, mkCtx := setupFullMailTestDB(t)
	wipeMailTables(t, p)
	ctx := context.Background()

	host, port, gotOrg10 := captureSMTP(t)
	mv := p.vaultSvc.(*mockVaultService)

	org10Sender, err := seedVaultSMTPSender(p, SMTPSender{
		OrgID:     10,
		Name:      "Org 10 SMTP",
		Host:      host,
		Port:      port,
		User:      "org10-user",
		FromEmail: "org10@octarq.com",
	}, "p10")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mv.PutSecret(ctx, plugin.PutSecretRequest{
		OrgID:        20,
		Scope:        plugin.VaultScopeOrg,
		ItemType:     "integration",
		ProviderType: "communication",
		Provider:     "smtp",
		Name:         "Org 20 SMTP",
		Payload: map[string]any{
			"host":      host,
			"port":      port,
			"user":      "org20-user",
			"fromEmail": "org20@octarq.com",
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Probe 1: Org 20 calls sendEmail referencing Org 10's SMTPSenderID
	reqOrg20 := httptest.NewRequest(http.MethodPost, "/api/emails/send", nil)
	reqOrg20.Header.Set("X-Org-ID", "20")
	inputCross := &SendEmailInput{
		Ctx: mkCtx(reqOrg20),
		Body: struct {
			internalmail.Message
			SMTPSenderID     uint   `json:"smtpSenderId"`
			SMTPSenderSource string `json:"smtpSenderSource"`
			TrackLinks       bool   `json:"trackLinks"`
		}{
			SMTPSenderID:     org10Sender.ID,
			SMTPSenderSource: "local",
			Message: internalmail.Message{
				To:      []string{"victim@example.com"},
				Subject: "Cross Tenant Attack",
				Text:    "Unauthorized email body",
			},
		},
	}
	_, err = p.sendEmail(ctx, inputCross)
	if err == nil {
		t.Fatalf("CRITICAL SECURITY VULNERABILITY: Org 20 successfully sent an email using Org 10's SMTP sender!")
	}
	if !strings.Contains(err.Error(), "could not be found for this workspace") {
		t.Errorf("expected 400 'could not be found for this workspace', got: %v", err)
	}

	// Verify SMTP relay did NOT receive any message with Org 10 sender
	captured := gotOrg10()
	if strings.Contains(captured, "Cross Tenant Attack") {
		t.Errorf("CRITICAL LEAKAGE: Captured message from unauthorized send: %s", captured)
	}

	// Probe 2: sendMail for Org 30 (which has no credentials configured)
	err = p.sendMail(30, "unauthorized@example.com", "Subject", "Body", "Body")
	if err == nil {
		t.Fatalf("CRITICAL SECURITY VULNERABILITY: sendMail succeeded for Org 30 with no configured senders!")
	}
	if !strings.Contains(err.Error(), "no SMTP or Resend integration is configured for org 30") {
		t.Errorf("expected 'no SMTP or Resend integration is configured for org 30', got: %v", err)
	}
}

// TestChallenger_MailVaultSources tests sending via Vault integration and local Vault-backed sender.
func TestChallenger_MailVaultSources(t *testing.T) {
	p, mkCtx := setupFullMailTestDB(t)
	wipeMailTables(t, p)
	ctx := context.Background()

	hostSPI, portSPI, gotCaptureSPI := captureSMTP(t)
	hostLocal, portLocal, gotCaptureLocal := captureSMTP(t)
	mv := p.vaultSvc.(*mockVaultService)

	localSender, err := seedVaultSMTPSender(p, SMTPSender{
		ID:        50,
		OrgID:     10,
		Name:      "Local OSS Sender",
		Host:      hostLocal,
		Port:      portLocal,
		User:      "local-user",
		FromEmail: "local-sender@octarq.com",
	}, "local-pass")
	if err != nil {
		t.Fatal(err)
	}

	intMeta, err := mv.PutSecret(ctx, plugin.PutSecretRequest{
		OrgID:        10,
		Scope:        plugin.VaultScopeOrg,
		ItemType:     "integration",
		ProviderType: "communication",
		Provider:     "smtp",
		Name:         "Vault Integration Sender",
		Payload: map[string]any{
			"host":      hostSPI,
			"port":      portSPI,
			"user":      "spi-user",
			"fromEmail": "spi-sender@octarq.com",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/emails/send", nil)
	req.Header.Set("X-Org-ID", "10")

	// 1. Send via Vault integration ID
	inputSPI := &SendEmailInput{
		Ctx: mkCtx(req),
		Body: struct {
			internalmail.Message
			SMTPSenderID     uint   `json:"smtpSenderId"`
			SMTPSenderSource string `json:"smtpSenderSource"`
			TrackLinks       bool   `json:"trackLinks"`
		}{
			SMTPSenderID:     intMeta.ID,
			SMTPSenderSource: "integration",
			Message: internalmail.Message{
				To:      []string{"recipient1@example.com"},
				Subject: "SPI Subject",
				Text:    "Body",
			},
		},
	}
	outSPI, err := p.sendEmail(ctx, inputSPI)
	if err != nil || !outSPI.Body["ok"] {
		t.Fatalf("sendEmail via Vault integration failed: %v", err)
	}

	// 2. Send via Local Table sender
	inputLocal := &SendEmailInput{
		Ctx: mkCtx(req),
		Body: struct {
			internalmail.Message
			SMTPSenderID     uint   `json:"smtpSenderId"`
			SMTPSenderSource string `json:"smtpSenderSource"`
			TrackLinks       bool   `json:"trackLinks"`
		}{
			SMTPSenderID:     localSender.ID,
			SMTPSenderSource: "local",
			Message: internalmail.Message{
				To:      []string{"recipient2@example.com"},
				Subject: "Local Subject",
				Text:    "Body",
			},
		},
	}
	outLocal, err := p.sendEmail(ctx, inputLocal)
	if err != nil || !outLocal.Body["ok"] {
		t.Fatalf("sendEmail via local sender failed: %v", err)
	}

	// 3. Absent ID 9999 -> 400 Bad Request
	inputAbsent := &SendEmailInput{
		Ctx: mkCtx(req),
		Body: struct {
			internalmail.Message
			SMTPSenderID     uint   `json:"smtpSenderId"`
			SMTPSenderSource string `json:"smtpSenderSource"`
			TrackLinks       bool   `json:"trackLinks"`
		}{
			SMTPSenderID:     9999,
			SMTPSenderSource: "local",
			Message: internalmail.Message{
				To:      []string{"recipient3@example.com"},
				Subject: "Absent",
				Text:    "Body",
			},
		},
	}
	_, err = p.sendEmail(ctx, inputAbsent)
	if err == nil {
		t.Fatalf("expected 400 for absent sender, got success")
	}

	capturedSPI := gotCaptureSPI()
	if !strings.Contains(capturedSPI, "spi-sender@octarq.com") {
		t.Errorf("captured stream missing spi-sender: %s", capturedSPI)
	}
	capturedLocal := gotCaptureLocal()
	if !strings.Contains(capturedLocal, "local-sender@octarq.com") {
		t.Errorf("captured stream missing local-sender: %s", capturedLocal)
	}
}

// TestChallenger_MailInvalidConfigurationFailClosed tests that an invalid Vault
// integration fails closed and never falls through to a local sender.
func TestChallenger_MailInvalidConfigurationFailClosed(t *testing.T) {
	p, mkCtx := setupFullMailTestDB(t)
	wipeMailTables(t, p)
	ctx := context.Background()
	mv := p.vaultSvc.(*mockVaultService)

	badResend, err := mv.PutSecret(ctx, plugin.PutSecretRequest{
		OrgID:        10,
		Scope:        plugin.VaultScopeOrg,
		ItemType:     "integration",
		ProviderType: "communication",
		Provider:     "resend",
		Name:         "Broken Resend",
		Payload:      map[string]any{"apiKey": "", "fromEmail": "broken@octarq.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/emails/send", nil)
	req.Header.Set("X-Org-ID", "10")

	input := &SendEmailInput{
		Ctx: mkCtx(req),
		Body: struct {
			internalmail.Message
			SMTPSenderID     uint   `json:"smtpSenderId"`
			SMTPSenderSource string `json:"smtpSenderSource"`
			TrackLinks       bool   `json:"trackLinks"`
		}{
			SMTPSenderID:     badResend.ID,
			SMTPSenderSource: "integration",
			Message: internalmail.Message{
				To:      []string{"customer@example.com"},
				Subject: "Fail Closed Test",
				Text:    "Test Body",
			},
		},
	}
	if _, err := p.sendEmail(ctx, input); err == nil {
		t.Fatal("expected invalid Resend integration to fail closed")
	}
}

// TestChallenger_MailReadyCheck tests mailReady against the explicitly configured
// system sender.
func TestChallenger_MailReadyCheck(t *testing.T) {
	p, _ := setupFullMailTestDB(t)
	wipeMailTables(t, p)
	mv := p.vaultSvc.(*mockVaultService)

	// 1. Initial state: no system sender -> mailReady = false
	if p.mailReady() {
		t.Errorf("expected mailReady = false with no senders")
	}

	// 2. Org-scoped integration does not make mailReady true.
	if _, err := mv.PutSecret(context.Background(), plugin.PutSecretRequest{
		OrgID:        1,
		Scope:        plugin.VaultScopeOrg,
		ItemType:     "integration",
		ProviderType: "communication",
		Provider:     "smtp",
		Name:         "Org SMTP",
		Payload:      map[string]any{"smtpHost": "smtp.example.com", "secretKey": "pw"},
	}); err != nil {
		t.Fatal(err)
	}
	if p.mailReady() {
		t.Errorf("expected mailReady = false when only an org-scoped integration exists")
	}

	// 3. Selecting a system sender makes system mail deliverable.
	sender, err := seedVaultSMTPSender(p, SMTPSender{
		OrgID:     1,
		Name:      "Local",
		Host:      "localhost",
		Port:      25,
		FromEmail: "test@octarq.com",
	}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	p.getGlobalSetting = func(k string) string {
		if k == "mail_system_sender_id" {
			return fmt.Sprint(sender.ID)
		}
		return ""
	}
	if !p.mailReady() {
		t.Errorf("expected mailReady = true when the system sender is selected")
	}
}
