package mail

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

func setupMailboxTestDB(t *testing.T) (*gorm.DB, *Plugin) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	p := New()
	p.vaultSvc = newMockVaultService()
	if err := db.AutoMigrate(append(testAllModels(), p.Models()...)...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Where("1 = 1").Delete(&Mailbox{})
	db.Where("1 = 1").Delete(&Email{})
	db.Where("1 = 1").Delete(&testDomain{})
	db.Where("1 = 1").Delete(&testOrg{})

	p.Mount(nil, &plugin.Context{
		DB: db,
		Host: &plugin.TestHost{
			SessionMock: &plugin.TestSession{
				OrgIDFn: func(r *http.Request) uint {
					if val := r.Header.Get("X-Org-ID"); val != "" {
						var id uint
						fmt.Sscanf(val, "%d", &id)
						return id
					}
					return 1
				},
			},
		},
	})
	return db, p
}

func TestMailboxScopeAndDomainValidation(t *testing.T) {
	db, p := setupMailboxTestDB(t)

	db.Create(&testDomain{
		OrgID:   2,
		Name:    "victim.example",
		ForMail: true,
		MailHosts: hostlist.HostList{
			hostlist.Host{Host: "victim.example", Enabled: true},
			hostlist.Host{Host: "shared.example", Enabled: true},
		},
	})

	db.Create(&testDomain{
		OrgID:   1,
		Name:    "mymail.example",
		ForMail: true,
		MailHosts: hostlist.HostList{
			hostlist.Host{Host: "mymail.example", Enabled: true},
			hostlist.Host{Host: "shared.example", Enabled: true},
		},
	})

	ctx := context.Background()

	{
		req := httptest.NewRequest(http.MethodPost, "/api/mailboxes", nil)
		req.Header.Set("X-Org-ID", "1")
		input := &CreateMailboxInput{
			Ctx: humago.NewContext(nil, req, httptest.NewRecorder()),
			Body: mailboxDTO{
				Address: "user@mymail.example",
			},
		}
		out, err := p.createMailbox(ctx, input)
		if err != nil {
			t.Fatalf("expected success creating mailbox for Org A, got: %v", err)
		}
		if out.Body.Address != "user@mymail.example" {
			t.Errorf("got %q, want user@mymail.example", out.Body.Address)
		}
	}

	{
		req1 := httptest.NewRequest(http.MethodPost, "/api/mailboxes", nil)
		req1.Header.Set("X-Org-ID", "1")
		input1 := &CreateMailboxInput{
			Ctx: humago.NewContext(nil, req1, httptest.NewRecorder()),
			Body: mailboxDTO{
				Address: "same@unmanaged.example",
			},
		}
		if _, err := p.createMailbox(ctx, input1); err != nil {
			t.Fatalf("Org A failed to create mailbox on an unclaimed domain: %v", err)
		}

		req2 := httptest.NewRequest(http.MethodPost, "/api/mailboxes", nil)
		req2.Header.Set("X-Org-ID", "2")
		input2 := &CreateMailboxInput{
			Ctx: humago.NewContext(nil, req2, httptest.NewRecorder()),
			Body: mailboxDTO{
				Address: "same@unmanaged.example",
			},
		}
		if _, err := p.createMailbox(ctx, input2); err != nil {
			t.Fatalf("Org B failed to create same address on unmanaged domain: %v", err)
		}
	}

	{
		req := httptest.NewRequest(http.MethodPost, "/api/mailboxes", nil)
		req.Header.Set("X-Org-ID", "1")
		input := &CreateMailboxInput{
			Ctx: humago.NewContext(nil, req, httptest.NewRecorder()),
			Body: mailboxDTO{
				Address: "billing@victim.example",
			},
		}
		_, err := p.createMailbox(ctx, input)
		if err == nil {
			t.Fatal("expected 403 error when Org A creates mailbox on Org B's domain, got nil")
			return
		}
	}
}

func TestCatchAllOnlyCreatesOnOwnedMailHosts(t *testing.T) {
	db, p := setupMailboxTestDB(t)
	p.getWorkspaceSetting = func(orgID uint, key string) string {
		if key == "catch_all" {
			return "true"
		}
		return ""
	}

	const orgA uint = 1
	db.Create(&testDomain{
		OrgID:   orgA,
		Name:    "owned.example",
		ForMail: true,
		MailHosts: hostlist.HostList{
			hostlist.Host{Host: "owned.example", Enabled: true},
		},
	})

	if _, ok := p.resolveMailbox(orgA, "anyone@owned.example"); !ok {
		t.Fatal("catch-all should create a mailbox on the workspace's own mail host")
	}

	if _, ok := p.resolveMailbox(orgA, "anyone@unclaimed.example"); ok {
		t.Fatal("catch-all must not create a mailbox on a domain the workspace does not own")
	}
}

func TestDeleteMailboxTenantIsolation(t *testing.T) {
	db, p := setupMailboxTestDB(t)
	ctx := context.Background()

	mbOrg2 := Mailbox{
		OrgID:   2,
		Address: "secret@victim.example",
		Enabled: true,
	}
	if err := db.Create(&mbOrg2).Error; err != nil {
		t.Fatalf("create mbOrg2: %v", err)
	}
	emailOrg2 := Email{
		MailboxID:  mbOrg2.ID,
		MessageID:  "msg-secret",
		Subject:    "Confidential",
		StorageKey: "mail/2/confidential.eml",
	}
	if err := db.Create(&emailOrg2).Error; err != nil {
		t.Fatalf("create emailOrg2: %v", err)
	}

	var emailQueryIntercepted bool
	_ = db.Callback().Query().Before("gorm:query").Register("test_intercept_email_query", func(d *gorm.DB) {
		if d.Statement.Table == "emails" || (d.Statement.Schema != nil && d.Statement.Schema.Table == "emails") {
			emailQueryIntercepted = true
		}
	})

	req1 := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/mailboxes/%d", mbOrg2.ID), nil)
	req1.Header.Set("X-Org-ID", "1")
	input1 := &DeleteMailboxInput{
		Ctx: humago.NewContext(nil, req1, httptest.NewRecorder()),
		ID:  mbOrg2.ID,
	}
	_, err := p.deleteMailbox(ctx, input1)
	if err == nil {
		t.Fatal("expected error when Org 1 deletes Org 2 mailbox, got nil")
	}
	if emailQueryIntercepted {
		t.Fatal("emails table was queried before verifying mailbox tenant ownership")
	}

	var mbCount int64
	db.Model(&Mailbox{}).Where("id = ?", mbOrg2.ID).Count(&mbCount)
	if mbCount == 0 {
		t.Fatal("Org 2 mailbox was deleted by Org 1")
	}
	var emailCount int64
	db.Model(&Email{}).Where("mailbox_id = ?", mbOrg2.ID).Count(&emailCount)
	if emailCount == 0 {
		t.Fatal("Org 2 email was deleted by Org 1")
	}

	req2 := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/mailboxes/%d", mbOrg2.ID), nil)
	req2.Header.Set("X-Org-ID", "2")
	input2 := &DeleteMailboxInput{
		Ctx: humago.NewContext(nil, req2, httptest.NewRecorder()),
		ID:  mbOrg2.ID,
	}
	_, err = p.deleteMailbox(ctx, input2)
	if err != nil {
		t.Fatalf("expected Org 2 to delete own mailbox, got error: %v", err)
	}

	db.Model(&Mailbox{}).Where("id = ?", mbOrg2.ID).Count(&mbCount)
	if mbCount != 0 {
		t.Fatalf("Org 2 mailbox should have been deleted, count = %d", mbCount)
	}
	db.Model(&Email{}).Where("mailbox_id = ?", mbOrg2.ID).Count(&emailCount)
	if emailCount != 0 {
		t.Fatalf("Org 2 emails should have been cascaded, count = %d", emailCount)
	}
}
