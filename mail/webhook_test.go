package mail

import (
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

func TestEmitEmailFansOut(t *testing.T) {
	p := &Plugin{}

	var mu sync.Mutex
	got := map[string]plugin.EmailEvent{}
	var wg sync.WaitGroup
	wg.Add(2)

	p.OnEmail(func(e plugin.EmailEvent) {
		mu.Lock()
		got["a"] = e
		mu.Unlock()
		wg.Done()
	})
	p.OnEmail(func(e plugin.EmailEvent) {
		mu.Lock()
		got["b"] = e
		mu.Unlock()
		wg.Done()
	})

	want := plugin.EmailEvent{ID: 7, OrgID: 3, From: "x@y.z", Subject: "hi"}
	p.emitEmail(want)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handlers did not fire")
	}

	mu.Lock()
	defer mu.Unlock()
	if got["a"].ID != 7 || got["b"].ID != 7 || got["a"].Subject != "hi" {
		t.Errorf("handlers got wrong event: %+v", got)
	}
}

func TestOnEmailNilIgnored(t *testing.T) {
	p := &Plugin{}
	p.OnEmail(nil)
	p.emitEmail(plugin.EmailEvent{ID: 1})
}

func TestMailDispatcherProvided(t *testing.T) {
	p := New()
	reg := plugin.NewRegistry()
	p.Mount(nil, &plugin.Context{
		Provide: reg.Provide,
		Lookup:  reg.Lookup,
	})

	svc, ok := reg.Lookup(plugin.ServiceMailDispatcher)
	if !ok || svc == nil {
		t.Fatal("mail.dispatcher service was not provided during Mount")
		return
	}
	if _, isDispatcher := svc.(plugin.EmailDispatcher); !isDispatcher {
		t.Fatalf("mail.dispatcher service has type %T, want plugin.EmailDispatcher", svc)
	}
}

func TestOwnsMailHostSecurityBoundary(t *testing.T) {
	p := &Plugin{}

	if p.ownsMailHost(0, "user@example.com") {
		t.Error("ownsMailHost with orgID=0 expected false")
	}
	if p.ownsMailHost(1, "user@example.com") {
		t.Error("ownsMailHost with nil db expected false")
	}

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&Mailbox{}, &testDomain{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	p.db = db

	invalidAddrs := []string{"invalid-addr", "user@", "@domain.com", ""}
	for _, addr := range invalidAddrs {
		if p.ownsMailHost(1, addr) {
			t.Errorf("ownsMailHost(%q) expected false", addr)
		}
	}

	db.Create(&testDomain{
		OrgID:   1,
		Name:    "nomail.com",
		ForMail: false,
	})
	if p.ownsMailHost(1, "info@nomail.com") {
		t.Error("ownsMailHost for domain with ForMail=false expected false")
	}

	db.Create(&testDomain{
		OrgID:   1,
		Name:    "apexmail.com",
		ForMail: true,
	})
	if !p.ownsMailHost(1, "info@apexmail.com") {
		t.Error("ownsMailHost for apex domain expected true")
	}

	if p.ownsMailHost(2, "info@apexmail.com") {
		t.Error("ownsMailHost for Org 2 on Org 1's domain expected false (cross-tenant breach)")
	}

	db.Create(&testDomain{
		OrgID:   1,
		Name:    "custom.com",
		ForMail: true,
		MailHosts: hostlist.HostList{
			{Host: "sub.custom.com", Enabled: true},
		},
	})
	if !p.ownsMailHost(1, "info@sub.custom.com") {
		t.Error("ownsMailHost for explicit mail host expected true")
	}
	if p.ownsMailHost(1, "info@unlisted.custom.com") {
		t.Error("ownsMailHost for unlisted mail host expected false")
	}
}
