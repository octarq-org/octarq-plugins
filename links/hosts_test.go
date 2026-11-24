package links

import (
	"context"
	"testing"
)

func TestHostResolution_CustomHostResolvesToCorrectOrg(t *testing.T) {
	db := newGuardDB(t)
	seedOrgs(t, db, 2)

	hosts := newFakeHostRegistry()
	if err := hosts.ReplaceOwned(context.Background(), 1, "dns.domain.1", []string{"custom.example.com"}, true); err != nil {
		t.Fatalf("bind host: %v", err)
	}

	link1 := &Link{OrgID: 1, Slug: "promo", Host: "custom.example.com", Target: "https://org1.example/promo", Enabled: true}
	if err := db.Create(link1).Error; err != nil {
		t.Fatalf("create org 1 link: %v", err)
	}

	link2 := &Link{OrgID: 2, Slug: "promo", Host: "", Target: "https://org2.example/promo", Enabled: true}
	if err := db.Create(link2).Error; err != nil {
		t.Fatalf("create org 2 link: %v", err)
	}

	eng := NewEngine(db, mockCtx())
	eng.SetHosts(hosts)

	got, ok := eng.Lookup("custom.example.com", "promo")
	if !ok {
		t.Fatal("expected link to resolve on custom host")
	}
	if got.OrgID != 1 || got.Target != "https://org1.example/promo" {
		t.Fatalf("expected org 1 link, got org=%d target=%q", got.OrgID, got.Target)
	}

	if _, ok := eng.Lookup("custom.example.com", "nonexistent"); ok {
		t.Fatal("expected non-existent slug to not resolve")
	}
}

func TestHostResolution_AmbiguousHostRefuses(t *testing.T) {
	db := newGuardDB(t)
	seedOrgs(t, db, 2)

	hosts := newFakeHostRegistry()
	hosts.setAmbiguous("ambiguous.example.com", true)

	link1 := &Link{OrgID: 1, Slug: "deal", Host: "ambiguous.example.com", Target: "https://org1.example/deal", Enabled: true}
	if err := db.Create(link1).Error; err != nil {
		t.Fatalf("create org 1 link: %v", err)
	}
	link2 := &Link{OrgID: 2, Slug: "deal", Host: "", Target: "https://org2.example/deal", Enabled: true}
	if err := db.Create(link2).Error; err != nil {
		t.Fatalf("create org 2 link: %v", err)
	}

	eng := NewEngine(db, mockCtx())
	eng.SetHosts(hosts)

	if _, ok := eng.Lookup("ambiguous.example.com", "deal"); ok {
		t.Fatal("expected ambiguous host lookup to be refused")
	}
}

func TestHostResolution_UnboundHostDoesNotServe(t *testing.T) {
	db := newGuardDB(t)
	seedOrgs(t, db, 2)

	hosts := newFakeHostRegistry()

	link := &Link{OrgID: 1, Slug: "test", Host: "unbound.example.com", Target: "https://org1.example/test", Enabled: true}
	if err := db.Create(link).Error; err != nil {
		t.Fatalf("create link: %v", err)
	}

	eng := NewEngine(db, mockCtx())
	eng.SetHosts(hosts)

	if _, ok := eng.Lookup("unbound.example.com", "test"); ok {
		t.Fatal("expected unbound host link to not be served")
	}
}
