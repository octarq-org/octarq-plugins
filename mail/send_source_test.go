package mail

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	internalmail "github.com/octarq-org/octarq-plugins/mail/pkg/mail"
	"github.com/octarq-org/octarq/server/plugin"
)

func buildSendInput(ctx huma.Context, id uint, source string) *SendEmailInput {
	return &SendEmailInput{
		Ctx: ctx,
		Body: struct {
			internalmail.Message
			SMTPSenderID     uint   `json:"smtpSenderId"`
			SMTPSenderSource string `json:"smtpSenderSource"`
			TrackLinks       bool   `json:"trackLinks"`
		}{
			SMTPSenderID:     id,
			SMTPSenderSource: source,
			Message:          internalmail.Message{To: []string{"to@example.com"}, Subject: "s", Text: "b"},
		},
	}
}

func sendReq(t *testing.T) huma.Context {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/emails/send", nil)
	req.Header.Set("X-Org-ID", "1")
	return humago.NewContext(nil, req, httptest.NewRecorder())
}

func TestSendEmailSourceScoping(t *testing.T) {
	p, _ := setupFullMailTestDB(t)
	wipeMailTables(t, p)
	ctx := context.Background()
	p.publishEvent = func(uint, string, any) {}
	mv := p.vaultSvc.(*mockVaultService)

	localHost, localPort, localGot := captureSMTP(t)
	local, err := seedVaultSMTPSender(p, SMTPSender{OrgID: 1, Name: "local relay", Host: localHost, Port: localPort, User: "u", FromEmail: "local@example.com"}, "pw")
	if err != nil {
		t.Fatalf("seed local sender: %v", err)
	}

	hubHost, hubPort, hubGot := captureSMTP(t)
	hubMeta, err := mv.PutSecret(ctx, plugin.PutSecretRequest{
		OrgID:        1,
		Scope:        plugin.VaultScopeOrg,
		ItemType:     "integration",
		ProviderType: "communication",
		Provider:     "smtp",
		Name:         "Vault Integration",
		Payload: map[string]any{
			"host":      hubHost,
			"port":      hubPort,
			"user":      "hub-user",
			"fromEmail": "hub@example.com",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.sendEmail(ctx, buildSendInput(sendReq(t), local.ID, "local")); err != nil {
		t.Fatalf("local send: %v", err)
	}
	if !strings.Contains(localGot(), "local@example.com") {
		t.Errorf("local relay did not receive the message:\n%s", localGot())
	}

	if _, err := p.sendEmail(ctx, buildSendInput(sendReq(t), hubMeta.ID, "integration")); err != nil {
		t.Fatalf("integration send: %v", err)
	}
	if !strings.Contains(hubGot(), "hub@example.com") {
		t.Errorf("hub relay did not receive the message:\n%s", hubGot())
	}

	orphanHost, orphanPort, orphanGot := captureSMTP(t)
	orphan, err := seedVaultSMTPSender(p, SMTPSender{OrgID: 1, Name: "orphan", Host: orphanHost, Port: orphanPort, User: "u", FromEmail: "orphan@example.com"}, "pw")
	if err != nil {
		t.Fatalf("seed orphan sender: %v", err)
	}
	if _, err := p.sendEmail(ctx, buildSendInput(sendReq(t), orphan.ID, "integration")); err == nil {
		t.Fatal("expected an error for an absent integration ID")
	}
	if strings.Contains(orphanGot(), "orphan@example.com") {
		t.Error("an absent integration must not fall back to the local sender table")
	}

	for _, source := range []string{"", "hub"} {
		if _, err := p.sendEmail(ctx, buildSendInput(sendReq(t), local.ID, source)); err == nil {
			t.Errorf("source %q must be refused", source)
		}
	}
}

func TestSendEmailIntegrationRejectsNonMailProvider(t *testing.T) {
	p, _ := setupFullMailTestDB(t)
	wipeMailTables(t, p)
	ctx := context.Background()
	mv := p.vaultSvc.(*mockVaultService)

	meta, err := mv.PutSecret(ctx, plugin.PutSecretRequest{
		OrgID:        1,
		Scope:        plugin.VaultScopeOrg,
		ItemType:     "integration",
		ProviderType: "communication",
		Provider:     "telegram",
		Name:         "Telegram",
		Payload:      map[string]any{"botToken": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.sendEmail(ctx, buildSendInput(sendReq(t), meta.ID, "integration")); err == nil {
		t.Fatal("a Telegram integration must not be used as a mail sender")
	}
}

func TestSendEmailIntegrationRequiresVault(t *testing.T) {
	p, _ := setupFullMailTestDB(t)
	wipeMailTables(t, p)
	ctx := context.Background()
	p.vaultSvc = nil

	if _, err := p.sendEmail(ctx, buildSendInput(sendReq(t), 5, "integration")); err == nil {
		t.Fatal("an integration source must fail closed when no vault is mounted")
	}
}
