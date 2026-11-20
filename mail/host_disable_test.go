package mail

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"gorm.io/gorm"
)

func TestMailHostDisabledTenantIsolation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&Mailbox{}, &testDomain{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Where("1 = 1").Delete(&Mailbox{})
	db.Where("1 = 1").Delete(&testDomain{})

	db.Create(&testDomain{
		OrgID:   2,
		Name:    "victim.com",
		ForMail: true,
		MailHosts: hostlist.HostList{
			hostlist.Host{Host: "x.example", Enabled: true},
		},
	})

	db.Create(&testDomain{
		OrgID:   1,
		Name:    "attacker.com",
		ForMail: true,
		MailHosts: hostlist.HostList{
			hostlist.Host{Host: "x.example", Enabled: false},
		},
	})

	p := &Plugin{db: db}

	if p.mailHostDisabled(2, "x.example") {
		t.Fatal("org A disabling x.example must not disable mail for org B")
	}

	if !p.mailHostDisabled(1, "x.example") {
		t.Fatal("org A disabled x.example on its own domain; it must read as disabled for A")
	}

	if p.mailHostDisabled(1, "unlisted.example") {
		t.Fatal("an unlisted host must not be reported as disabled")
	}
}
