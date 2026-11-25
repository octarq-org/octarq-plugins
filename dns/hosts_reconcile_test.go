package dns

import (
	"context"
	"testing"

	"github.com/octarq-org/octarq/server/plugin"
)

func TestReconcileDomainHostsBindsExistingDomains(t *testing.T) {
	p, fake, _ := setupSyncTestPlugin(t)

	p.db.Create(&Domain{OrgID: 1, Name: "acme.org", ForLink: true})
	p.db.Create(&Domain{OrgID: 2, Name: "globex.dev", ForMail: true})

	p.reconcileDomainHosts()

	if owner, ok := fake.OwnerOf("acme.org"); !ok || owner != 1 {
		t.Fatalf("acme.org owner = %d, ok = %v; want org 1", owner, ok)
	}
	if !fake.ServesTraffic("acme.org") {
		t.Fatal("acme.org must serve traffic because the domain is used for links")
	}
	if owner, ok := fake.OwnerOf("globex.dev"); !ok || owner != 2 {
		t.Fatalf("globex.dev owner = %d, ok = %v; want org 2", owner, ok)
	}
	if fake.ServesTraffic("globex.dev") {
		t.Fatal("globex.dev must not serve traffic because it is mail-only")
	}
}

func TestReconcileDomainHostsKeepsGoingPastConflicts(t *testing.T) {
	p, fake, _ := setupSyncTestPlugin(t)

	if err := fake.ReplaceOwned(context.Background(), 2, "other.source", []string{"taken.org"}, false); err != nil {
		t.Fatalf("pre-bind host: %v", err)
	}
	p.db.Create(&Domain{OrgID: 1, Name: "taken.org", ForLink: true})
	p.db.Create(&Domain{OrgID: 1, Name: "free.org", ForLink: true})

	p.reconcileDomainHosts()

	if owner, _ := fake.OwnerOf("taken.org"); owner != 2 {
		t.Fatalf("taken.org owner = %d, want the pre-existing org 2", owner)
	}
	if owner, ok := fake.OwnerOf("free.org"); !ok || owner != 1 {
		t.Fatalf("free.org owner = %d, ok = %v; want org 1", owner, ok)
	}
}

func TestReconcileDomainHostsWithoutRegistryIsNoop(t *testing.T) {
	p, _, _ := setupSyncTestPlugin(t)
	p.SetHost(&plugin.TestHost{})
	p.db.Create(&Domain{OrgID: 1, Name: "acme.org", ForLink: true})

	p.reconcileDomainHosts()
}

func TestMountBindsExistingDomainHosts(t *testing.T) {
	p, fake, _ := setupSyncTestPlugin(t)
	p.db.Create(&Domain{OrgID: 3, Name: "mount.example", ForLink: true})

	reg := plugin.NewRegistry()
	p.Mount(nil, &plugin.Context{
		DB:      p.db,
		Host:    &plugin.TestHost{HostsMock: fake},
		Provide: reg.Provide,
		Lookup:  reg.Lookup,
	})

	if owner, ok := fake.OwnerOf("mount.example"); !ok || owner != 3 {
		t.Fatalf("mount.example owner = %d, ok = %v; want org 3 bound during mount", owner, ok)
	}
}
