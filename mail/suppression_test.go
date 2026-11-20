package mail

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTestPlugin(t *testing.T) (*Plugin, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	if err := db.AutoMigrate(append(testAllModels(), &Mailbox{}, &Email{}, &SMTPSender{}, &MailSuppression{})...); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	p := &Plugin{
		db:       db,
		vaultSvc: newMockVaultService(),
		orgID: func(r *http.Request) uint {
			return 1
		},
	}

	db.Create(&testOrg{ID: 1, Slug: "org1", InboundToken: "tok1"})
	db.Create(&testOrg{ID: 2, Slug: "org2", InboundToken: "tok2"})

	db.Create(&Mailbox{ID: 1, OrgID: 1, Address: "hardbounce@example.com", Enabled: true})
	db.Create(&Mailbox{ID: 2, OrgID: 1, Address: "softbounce@example.com", Enabled: true})
	db.Create(&Mailbox{ID: 3, OrgID: 1, Address: "complaint@example.com", Enabled: true})

	db.Create(&Mailbox{ID: 4, OrgID: 2, Address: "hardbounce@example.com", Enabled: true})

	return p, db
}

func mkCtx(req *http.Request) huma.Context {
	return humago.NewContext(nil, req, httptest.NewRecorder())
}

func callBounceWebhook(p *Plugin, orgSlug, token, jsonPayload string) (*EmailBounceWebhookOutput, error) {
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/webhook/%s/email/bounce/%s", orgSlug, token), bytes.NewBufferString(jsonPayload))
	input := &EmailBounceWebhookInput{
		Ctx:     mkCtx(req),
		OrgSlug: orgSlug,
		Token:   token,
	}
	return p.emailBounceWebhook(context.Background(), input)
}

func TestBounceTypePermanentEntersSuppressionList(t *testing.T) {
	p, db := setupTestPlugin(t)

	payload := `{
		"Type": "Notification",
		"Message": "{\"notificationType\":\"Bounce\",\"bounce\":{\"bounceType\":\"Permanent\",\"bounceSubType\":\"General\",\"bouncedRecipients\":[{\"emailAddress\":\"hardbounce@example.com\"}]}}"
	}`

	_, err := callBounceWebhook(p, "org1", "tok1", payload)
	if err != nil {
		t.Fatalf("webhook returned error: %v", err)
	}

	var item MailSuppression
	if err := db.Where("owner_id = ? AND address = ?", 1, "hardbounce@example.com").First(&item).Error; err != nil {
		t.Fatalf("expected address to enter suppression list, got error: %v", err)
	}
	if item.Reason != "hard_bounce" {
		t.Errorf("expected reason hard_bounce, got %s", item.Reason)
	}
}

func TestBounceTypeTransientDoesNotEnterSuppressionList(t *testing.T) {
	p, db := setupTestPlugin(t)

	payload := `{
		"Type": "Notification",
		"Message": "{\"notificationType\":\"Bounce\",\"bounce\":{\"bounceType\":\"Transient\",\"bounceSubType\":\"MailboxFull\",\"bouncedRecipients\":[{\"emailAddress\":\"softbounce@example.com\"}]}}"
	}`

	_, err := callBounceWebhook(p, "org1", "tok1", payload)
	if err != nil {
		t.Fatalf("webhook returned error: %v", err)
	}

	var count int64
	db.Model(&MailSuppression{}).Where("owner_id = ? AND address = ?", 1, "softbounce@example.com").Count(&count)
	if count != 0 {
		t.Errorf("transient bounce must NOT enter suppression list, but count was %d", count)
	}

	var auditCount int64
	db.Model(&testAuditLog{}).Where("org_id = ? AND action = ?", 1, "email.bounce").Count(&auditCount)
	if auditCount == 0 {
		t.Error("expected audit log to be written for transient bounce")
	}
}

func TestComplaintEntersSuppressionList(t *testing.T) {
	p, db := setupTestPlugin(t)

	payload := `{
		"Type": "Notification",
		"Message": "{\"notificationType\":\"Complaint\",\"complaint\":{\"complaintFeedbackType\":\"abuse\",\"complainedRecipients\":[{\"emailAddress\":\"complaint@example.com\"}]}}"
	}`

	_, err := callBounceWebhook(p, "org1", "tok1", payload)
	if err != nil {
		t.Fatalf("webhook returned error: %v", err)
	}

	var item MailSuppression
	if err := db.Where("owner_id = ? AND address = ?", 1, "complaint@example.com").First(&item).Error; err != nil {
		t.Fatalf("expected complaint address to enter suppression list, got error: %v", err)
	}
	if item.Reason != "complaint" {
		t.Errorf("expected reason complaint, got %s", item.Reason)
	}
}

func TestMultiplePermanentBouncesUpsertsSingleRecord(t *testing.T) {
	p, db := setupTestPlugin(t)

	payload := `{
		"Type": "Notification",
		"Message": "{\"notificationType\":\"Bounce\",\"bounce\":{\"bounceType\":\"Permanent\",\"bounceSubType\":\"General\",\"bouncedRecipients\":[{\"emailAddress\":\"hardbounce@example.com\"}]}}"
	}`

	for i := 0; i < 5; i++ {
		if _, err := callBounceWebhook(p, "org1", "tok1", payload); err != nil {
			t.Fatalf("webhook call %d failed: %v", i+1, err)
		}
	}

	var count int64
	db.Model(&MailSuppression{}).Where("owner_id = ? AND address = ?", 1, "hardbounce@example.com").Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 suppression row, got %d", count)
	}

	var item MailSuppression
	db.Where("owner_id = ? AND address = ?", 1, "hardbounce@example.com").First(&item)
	if item.Count != 5 {
		t.Errorf("expected count=5, got %d", item.Count)
	}
}

func TestSendInterceptionForSuppressedAddress(t *testing.T) {
	p, db := setupTestPlugin(t)

	db.Create(&MailSuppression{
		OrgID:   1,
		Address: "blocked@example.com",
		Reason:  "hard_bounce",
		Count:   1,
	})

	if _, err := seedVaultSMTPSender(p, SMTPSender{
		ID: 10, OrgID: 1, Name: "sup-relay", Host: "127.0.0.1", Port: 25, User: "u", FromEmail: "sender@example.com",
	}, "p"); err != nil {
		t.Fatal(err)
	}

	err := p.sendMail(1, "blocked@example.com", "Subject", "HTML", "Text")
	if err == nil {
		t.Fatal("expected sendMail to fail for suppressed address, but it succeeded")
		return
	}

	req := httptest.NewRequest("POST", "/api/emails/send", nil)
	input := &SendEmailInput{
		Ctx: mkCtx(req),
	}
	input.Body.SMTPSenderID = 10
	input.Body.To = []string{"blocked@example.com"}
	input.Body.Subject = "Subject"
	input.Body.Text = "Text"

	p.orgID = func(r *http.Request) uint { return 1 }

	_, sendErr := p.sendEmail(context.Background(), input)
	if sendErr == nil {
		t.Fatal("expected sendEmail to fail for suppressed address, but it succeeded")
		return
	}
}

func TestOrgIsolationForSuppressionList(t *testing.T) {
	p, db := setupTestPlugin(t)

	db.Create(&MailSuppression{
		OrgID:   1,
		Address: "x@y.com",
		Reason:  "hard_bounce",
		Count:   1,
	})

	if !p.isSuppressed(1, "x@y.com") {
		t.Error("expected x@y.com to be suppressed for Org 1")
	}

	if p.isSuppressed(2, "x@y.com") {
		t.Error("x@y.com MUST NOT be suppressed for Org 2 (Org isolation failure)")
	}
}
