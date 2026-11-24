package links

import (
	"context"
	"testing"

	"github.com/octarq-org/octarq/server/pkg/hostlist"
)

func TestResolveSlugAttributesByHost(t *testing.T) {
	db, p, engine := setupOwnershipTestDB(t)
	p.engine = engine
	hosts := newFakeHostRegistry()
	_ = hosts.ReplaceOwned(context.Background(), 1, "s1", []string{"victim.test"}, true)
	_ = hosts.ReplaceOwned(context.Background(), 2, "s2", []string{"other.test"}, true)
	engine.SetHosts(hosts)

	db.Create(&testDomain{OrgID: 1, Name: "victim.test", ForLink: true})
	db.Create(&testDomain{OrgID: 2, Name: "other.test", ForLink: true})
	db.Create(&Link{OrgID: 1, Slug: "dup", Host: "victim.test", Target: "https://victim.example", Enabled: true})
	db.Create(&Link{OrgID: 2, Slug: "dup", Host: "other.test", Target: "https://other.example", Enabled: true})

	target, orgID, ok := p.resolveSlug("victim.test", "dup")
	if !ok || orgID != 1 || target != "https://victim.example" {
		t.Fatalf("report on victim.test misattributed: org=%d target=%q ok=%v", orgID, target, ok)
	}

	target, orgID, ok = p.resolveSlug("other.test", "dup")
	if !ok || orgID != 2 || target != "https://other.example" {
		t.Fatalf("report on other.test misattributed: org=%d target=%q ok=%v", orgID, target, ok)
	}
}

func TestResolveSlugRefusesContestedHost(t *testing.T) {
	db, p, engine := setupOwnershipTestDB(t)
	p.engine = engine
	hosts := newFakeHostRegistry()
	hosts.setAmbiguous("victim.test", true)
	engine.SetHosts(hosts)

	db.Create(&testDomain{
		OrgID: 2, Name: "attacker.test", ForLink: true,
		LinkHosts: hostlist.HostList{hostlist.Host{Host: "victim.test", Enabled: true}},
	})
	db.Create(&testDomain{OrgID: 1, Name: "victim.test", ForLink: true})
	db.Create(&Link{OrgID: 1, Slug: "x", Host: "victim.test", Target: "https://victim.example", Enabled: true})

	if _, orgID, ok := p.resolveSlug("victim.test", "x"); ok {
		t.Fatalf("contested host attributed to org %d instead of refusing", orgID)
	}
}

func TestResolveSlugAttributesDisabledLink(t *testing.T) {
	db, p, engine := setupOwnershipTestDB(t)
	p.engine = engine

	db.Create(&Link{OrgID: 2, Slug: "off", Host: "", Target: "https://dest.example", Enabled: false})

	target, orgID, ok := p.resolveSlug("shared.example", "off")
	if !ok || orgID != 2 || target != "https://dest.example" {
		t.Fatalf("disabled link lost attribution: org=%d target=%q ok=%v", orgID, target, ok)
	}
}
