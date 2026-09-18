// Package maillink is the flagship "agent-native" Octarq demo plugin: when an
// email arrives, it auto-shortens the first link in the body (via the core
// links.create service) and exposes the results as an MCP tool, so an AI agent
// can fetch "the short link from my latest email" with no extra plumbing.
//
// It is ~a screenful of code and touches four public seams — OnEmail (inbound
// mail), LookupAs[plugin.LinkCreator] (cross-plugin service), a GORM Model, and
// MCPProvider — with no fork and no internal imports. That's the whole point:
// this is what a 10-line-feeling plugin can do on Octarq.
package maillink

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

// MailLink records a short link auto-created from an inbound email.
type MailLink struct {
	gorm.Model
	OrgID   uint `gorm:"index;column:org_id"`
	Slug    string
	Target  string
	Subject string
	From    string
}

func (MailLink) TenantColumn() string { return "org_id" }

var urlRe = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)

// Plugin is composed with app.Use(&maillink.Plugin{}).
type Plugin struct {
	ctx  *plugin.Context
	host plugin.Host
}

func (*Plugin) Name() string { return "maillink" }

func (*Plugin) Describe() plugin.Info {
	return plugin.Info{
		Title:       "Mail Links",
		Description: "Auto-shorten the first link in every inbound email, and expose them to AI agents over MCP.",
	}
}

func (*Plugin) Models() []any { return []any{&MailLink{}} }

func (p *Plugin) Mount(mux plugin.Mux, ctx *plugin.Context) {
	p.ctx = ctx
	p.host = plugin.EnsureHost(ctx)
	if p.host != nil && p.host.Events() != nil {
		p.host.Events().OnEmail(p.onEmail)
	}
	mux.Handle("GET /api/maillink/recent", ctx.Guard(http.HandlerFunc(p.recent)))
}

func (p *Plugin) Validate(ctx context.Context, host plugin.Host) error {
	if host == nil {
		return errors.New("maillink: host SPI is required")
	}
	return nil
}

// onEmail runs async after each inbound email: find the first URL, shorten it
// via the core links service, and record the result. It degrades to a no-op
// when links isn't composed into this build.
func (p *Plugin) onEmail(e plugin.EmailEvent) {
	if e.OrgID == 0 {
		return
	}
	body := e.Text
	if body == "" {
		body = e.HTML
	}
	target := urlRe.FindString(body)
	if target == "" {
		return
	}
	creator, ok := plugin.LookupAs[plugin.LinkCreator](p.ctx, "links.create")
	if !ok {
		return // links plugin absent — nothing to do
	}
	slug, err := creator.CreateLink(context.Background(), e.OrgID, target)
	if err != nil {
		return
	}
	if p.host != nil {
		if tdb := p.host.TenantDB(e.OrgID); tdb != nil {
			_ = tdb.Create(&MailLink{OrgID: e.OrgID, Slug: slug, Target: target, Subject: e.Subject, From: e.From}).Error
		}
	}
}

func (p *Plugin) recent(w http.ResponseWriter, r *http.Request) {
	var orgID uint
	if p.host != nil && p.host.Session() != nil {
		orgID = p.host.Session().OrgID(r)
	}
	if orgID == 0 {
		http.Error(w, "unauthorized workspace", http.StatusUnauthorized)
		return
	}
	writeJSON(w, p.list(r.Context(), orgID, 50))
}

func (p *Plugin) list(ctx context.Context, orgID uint, limit int) []MailLink {
	out := []MailLink{}
	if orgID == 0 || p.host == nil {
		return out
	}
	if tdb := p.host.TenantDB(orgID); tdb != nil {
		_ = tdb.Model(&MailLink{}).WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&out).Error
	}
	return out
}

// Compile-time assertions.
// Menus contributes the plugin's sidebar entry. It lives here, not on the
// frontend half: UIPlugin.menu was removed from the plugin SDK in 0.8.0, and a
// static frontend menu is ignored even where one is still declared.
func (*Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{ID: "maillink", Label: "Mail Links", Path: "/maillink", Icon: "🔗", Category: "Workspace"},
	}
}

var (
	_ plugin.Plugin       = (*Plugin)(nil)
	_ plugin.Validator    = (*Plugin)(nil)
	_ plugin.Describer    = (*Plugin)(nil)
	_ plugin.MCPProvider  = (*Plugin)(nil)
	_ plugin.MenuProvider = (*Plugin)(nil)
)
