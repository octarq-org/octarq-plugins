package mail

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq-plugins/mail/pkg/mail"
	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

type domainRow struct {
	ID        uint              `gorm:"primaryKey"`
	OrgID     uint              `gorm:"column:owner_id"`
	Name      string            `gorm:"column:name"`
	ForLink   bool              `gorm:"column:for_link"`
	LinkHosts hostlist.HostList `gorm:"column:link_hosts"`
}

type linkRow struct {
	ID      uint   `gorm:"primaryKey"`
	OrgID   uint   `gorm:"column:org_id"`
	Host    string `gorm:"column:host"`
	Slug    string `gorm:"column:slug"`
	Target  string `gorm:"column:target"`
	Enabled bool   `gorm:"column:enabled"`
}

type mockTestLinkCreator struct {
	db    *gorm.DB
	order []string
	fail  bool
}

func (m *mockTestLinkCreator) CreateLink(ctx context.Context, orgID uint, targetURL string) (string, error) {
	if m.fail {
		return "", errors.New("slug collision exhausted")
	}
	var existing linkRow
	if err := m.db.Table("links").Where("org_id = ? AND target = ?", orgID, targetURL).First(&existing).Error; err == nil {
		return existing.Slug, nil
	}
	slug := fmt.Sprintf("short%d", len(m.order)+1)
	m.order = append(m.order, targetURL)
	lk := linkRow{
		OrgID:   orgID,
		Host:    "",
		Slug:    slug,
		Target:  targetURL,
		Enabled: true,
	}
	if err := m.db.Table("links").Create(&lk).Error; err != nil {
		return "", err
	}
	return slug, nil
}

func TestWrapLinksInEmail(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("domains").AutoMigrate(&domainRow{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("links").AutoMigrate(&linkRow{}); err != nil {
		t.Fatal(err)
	}

	p := New()
	p.db = db
	p.orgID = func(r *http.Request) uint { return 1 }
	p.getWorkspaceSetting = func(orgID uint, key string) string { return "" }

	mockCreator := &mockTestLinkCreator{db: db}
	p.ctx = &plugin.Context{
		Lookup: func(name string) (any, bool) {
			if name == "links.create" {
				return mockCreator, true
			}
			return nil, false
		},
	}

	// Set up custom link domain
	db.Table("domains").Create(&domainRow{
		OrgID:   1,
		Name:    "short.mycorp.com",
		ForLink: true,
		LinkHosts: hostlist.HostList{
			{Host: "short.mycorp.com", Enabled: true},
		},
	})

	msg := mail.Message{
		Text: "Hello, visit our site at https://google.com for info. Also visit http://example.com/some/path.",
		HTML: `<p>Hello, visit <a href="https://google.com">Google</a> and <a href="http://example.com/some/path">Example</a>.</p>`,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/emails/send", nil)
	req.Host = "dashboard.mycorp.com"

	p.wrapLinksInEmail(req, &msg)

	// Assertions on text
	if !strings.Contains(msg.Text, "http://short.mycorp.com/") {
		t.Errorf("Text links not wrapped correctly: %q", msg.Text)
	}
	if strings.Contains(msg.Text, "https://google.com") {
		t.Errorf("Original Google URL still present in Text: %q", msg.Text)
	}

	// Assertions on HTML
	if !strings.Contains(msg.HTML, "http://short.mycorp.com/") {
		t.Errorf("HTML links not wrapped correctly: %q", msg.HTML)
	}
	if strings.Contains(msg.HTML, "https://google.com") {
		t.Errorf("Original Google URL still present in HTML: %q", msg.HTML)
	}

	// Check that links are stored in DB
	var links []linkRow
	db.Table("links").Find(&links)
	if len(links) != 2 {
		t.Errorf("expected 2 links generated in DB, got %d", len(links))
	}

	// Verify targets are correct
	targets := map[string]bool{
		"https://google.com":           true,
		"http://example.com/some/path": true,
	}
	for _, l := range links {
		if !targets[l.Target] {
			t.Errorf("unexpected target URL stored: %q", l.Target)
		}
		if l.Host != "" {
			t.Errorf("expected host-agnostic link (Host = ''), got %q", l.Host)
		}
	}
}

func TestWrapLinksAvoidDoubleWrapAndInternal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("domains").AutoMigrate(&domainRow{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("links").AutoMigrate(&linkRow{}); err != nil {
		t.Fatal(err)
	}

	p := New()
	p.db = db
	p.orgID = func(r *http.Request) uint { return 1 }
	p.getWorkspaceSetting = func(orgID uint, key string) string { return "" }

	mockCreator := &mockTestLinkCreator{db: db}
	p.ctx = &plugin.Context{
		Lookup: func(name string) (any, bool) {
			if name == "links.create" {
				return mockCreator, true
			}
			return nil, false
		},
	}

	db.Table("domains").Create(&domainRow{
		OrgID:   1,
		Name:    "avoid.mycorp.com",
		ForLink: true,
		LinkHosts: hostlist.HostList{
			{Host: "avoid.mycorp.com", Enabled: true},
		},
	})

	msg := mail.Message{
		Text: "Internal: http://localhost:8680/info. Already wrapped: http://avoid.mycorp.com/abc123. Normal: https://google.com",
	}

	req := httptest.NewRequest(http.MethodPost, "/api/emails/send", nil)
	req.Host = "avoid.mycorp.com"

	p.wrapLinksInEmail(req, &msg)

	if !strings.Contains(msg.Text, "http://localhost:8680/info") {
		t.Error("localhost link should not be wrapped")
	}
	if !strings.Contains(msg.Text, "http://avoid.mycorp.com/abc123") {
		t.Error("already wrapped short link should not be double wrapped")
	}
	if !strings.Contains(msg.Text, "http://avoid.mycorp.com/") || strings.Contains(msg.Text, "https://google.com") {
		t.Error("normal link should have been wrapped")
	}
}

func TestWrapLinksSlugExhaustionFallback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("domains").AutoMigrate(&domainRow{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("links").AutoMigrate(&linkRow{}); err != nil {
		t.Fatal(err)
	}

	p := New()
	p.db = db
	p.orgID = func(r *http.Request) uint { return 1 }
	p.getWorkspaceSetting = func(orgID uint, key string) string { return "" }

	mockCreator := &mockTestLinkCreator{db: db, fail: true}
	p.ctx = &plugin.Context{
		Lookup: func(name string) (any, bool) {
			if name == "links.create" {
				return mockCreator, true
			}
			return nil, false
		},
	}

	db.Table("domains").Create(&domainRow{
		OrgID:   1,
		Name:    "short.mycorp.com",
		ForLink: true,
		LinkHosts: hostlist.HostList{
			{Host: "short.mycorp.com", Enabled: true},
		},
	})

	msg := mail.Message{
		Text: "Hello, visit our site at https://example.com/target for info.",
		HTML: `<p>Hello, visit <a href="https://example.com/target">Example</a>.</p>`,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/emails/send", nil)
	req.Host = "dashboard.mycorp.com"

	p.wrapLinksInEmail(req, &msg)

	// Assertions: original URL returned without shortlink wrapping
	if !strings.Contains(msg.Text, "https://example.com/target") || strings.Contains(msg.Text, "short.mycorp.com") {
		t.Errorf("expected original URL in Text on slug exhaustion, got %q", msg.Text)
	}
	if !strings.Contains(msg.HTML, "https://example.com/target") || strings.Contains(msg.HTML, "short.mycorp.com") {
		t.Errorf("expected original URL in HTML on slug exhaustion, got %q", msg.HTML)
	}

	// Verify no Link rows were created in DB
	var allLinks []linkRow
	db.Table("links").Find(&allLinks)
	if len(allLinks) != 0 {
		t.Errorf("expected 0 links created, got %d", len(allLinks))
	}
}

func TestWrapLinksNoServiceGracefulFallback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("domains").AutoMigrate(&domainRow{}); err != nil {
		t.Fatal(err)
	}

	p := New()
	p.db = db
	p.orgID = func(r *http.Request) uint { return 1 }
	// p.ctx is nil, simulating absence of links plugin

	msg := mail.Message{
		Text: "Hello, visit https://google.com for info.",
		HTML: `<p>Visit <a href="https://google.com">Google</a></p>`,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/emails/send", nil)
	req.Host = "dashboard.mycorp.com"

	p.wrapLinksInEmail(req, &msg)

	if !strings.Contains(msg.Text, "https://google.com") {
		t.Errorf("expected unchanged Text when links service absent, got %q", msg.Text)
	}
	if !strings.Contains(msg.HTML, "https://google.com") {
		t.Errorf("expected unchanged HTML when links service absent, got %q", msg.HTML)
	}
}
