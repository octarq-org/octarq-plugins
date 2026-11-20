package links

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"gorm.io/gorm"
)

func newGuardDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&Link{}, &LinkEvent{}, &testDomain{}, &testProviderAccount{}, &testOrg{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedOrgs(t *testing.T, db *gorm.DB, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		if err := db.Create(&testOrg{
			Name: fmt.Sprintf("org-%d", i),
		}).Error; err != nil {
			t.Fatalf("seed org %d: %v", i, err)
		}
	}
}

func TestLookupServesHostAgnosticOnUnregisteredHost(t *testing.T) {
	db := newGuardDB(t)
	seedOrgs(t, db, 2)
	link := &Link{OrgID: 2, Slug: "promo", Host: "", Target: "https://org2-internal.example", Enabled: true}
	if err := db.Create(link).Error; err != nil {
		t.Fatalf("seed link: %v", err)
	}

	eng := NewEngine(db, mockCtx())
	got, ok := eng.Lookup("app.octarq.org", "promo")
	if !ok || got.ID != link.ID {
		t.Fatalf("host-agnostic link on an unregistered host: ok=%v, want found", ok)
	}
}

func TestLookupRefusesUnregisteredHostClaim(t *testing.T) {
	db := newGuardDB(t)
	seedOrgs(t, db, 2)
	if err := db.Create(&Link{OrgID: 1, Slug: "promo", Host: "claimed.example", Target: "https://dirty.example", Enabled: true}).Error; err != nil {
		t.Fatalf("seed link: %v", err)
	}

	eng := NewEngine(db, mockCtx())
	if _, ok := eng.Lookup("claimed.example", "promo"); ok {
		t.Fatal("exact-host link on an unregistered host resolved — unauthorized host claim served")
	}
}

func TestLookupRefusesContestedHost(t *testing.T) {
	for _, order := range []string{"victim-first", "attacker-first"} {
		t.Run(order, func(t *testing.T) {
			db := newGuardDB(t)
			seedOrgs(t, db, 2)

			victim := testDomain{OrgID: 1, Name: "victim.test", ForLink: true}
			attacker := testDomain{
				OrgID: 2, Name: "attacker.test", ForLink: true,
				LinkHosts: hostlist.HostList{{Host: "victim.test", Enabled: true}},
			}
			if order == "victim-first" {
				if err := db.Create(&victim).Error; err != nil {
					t.Fatalf("seed victim domain: %v", err)
				}
				if err := db.Create(&attacker).Error; err != nil {
					t.Fatalf("seed attacker domain: %v", err)
				}
			} else {
				if err := db.Create(&attacker).Error; err != nil {
					t.Fatalf("seed attacker domain: %v", err)
				}
				if err := db.Create(&victim).Error; err != nil {
					t.Fatalf("seed victim domain: %v", err)
				}
			}

			if err := db.Create(&Link{OrgID: 1, Slug: "promo", Host: "victim.test", Target: "https://legit.example", Enabled: true}).Error; err != nil {
				t.Fatalf("seed victim link: %v", err)
			}
			if err := db.Create(&Link{OrgID: 2, Slug: "promo", Host: "", Target: "https://phish.example", Enabled: true}).Error; err != nil {
				t.Fatalf("seed attacker link: %v", err)
			}

			eng := NewEngine(db, mockCtx())
			got, ok := eng.Lookup("victim.test", "promo")
			if ok {
				t.Fatalf("contested host resolved to org %d target %q — must refuse, not pick a side", got.OrgID, got.Target)
			}
		})
	}
}

func TestLookupScopesOwnedHostToOwner(t *testing.T) {
	db := newGuardDB(t)
	seedOrgs(t, db, 2)
	if err := db.Create(&testDomain{OrgID: 1, Name: "victim.test", ForLink: true}).Error; err != nil {
		t.Fatalf("seed victim domain: %v", err)
	}
	legit := &Link{OrgID: 1, Slug: "x", Host: "victim.test", Target: "https://legit.example", Enabled: true}
	if err := db.Create(legit).Error; err != nil {
		t.Fatalf("seed victim link: %v", err)
	}
	if err := db.Create(&Link{OrgID: 2, Slug: "y", Host: "", Target: "https://org2.example", Enabled: true}).Error; err != nil {
		t.Fatalf("seed org2 link: %v", err)
	}

	eng := NewEngine(db, mockCtx())

	got, ok := eng.Lookup("victim.test", "x")
	if !ok || got.ID != legit.ID {
		t.Fatalf("owner's own link on owned host: ok=%v, want found", ok)
	}

	if _, ok := eng.Lookup("victim.test", "y"); ok {
		t.Fatal("another org's host-agnostic link resolved on a host owned by a different org")
	}
}
