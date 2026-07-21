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
	"net/http"
	"regexp"

	"github.com/octarq-org/octarq/plugin"
	"gorm.io/gorm"
)

// MailLink records a short link auto-created from an inbound email.
type MailLink struct {
	gorm.Model
	OrgID   uint `gorm:"index"`
	Slug    string
	Target  string
	Subject string
	From    string
}

var urlRe = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)

// Plugin is composed with app.Use(&maillink.Plugin{}).
type Plugin struct {
	ctx *plugin.Context
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
	if ctx.OnEmail != nil {
		ctx.OnEmail(p.onEmail)
	}
	mux.Handle("GET /api/maillink/recent", ctx.Guard(http.HandlerFunc(p.recent)))
}

// onEmail runs async after each inbound email: find the first URL, shorten it
// via the core links service, and record the result. It degrades to a no-op
// when links isn't composed into this build.
func (p *Plugin) onEmail(e plugin.EmailEvent) {
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
	if p.ctx.DB != nil {
		_ = p.ctx.DB.Create(&MailLink{OrgID: e.OrgID, Slug: slug, Target: target, Subject: e.Subject, From: e.From}).Error
	}
}

func (p *Plugin) recent(w http.ResponseWriter, r *http.Request) {
	var orgID uint
	if p.ctx.OrgID != nil {
		orgID = p.ctx.OrgID(r)
	}
	writeJSON(w, p.list(r.Context(), orgID, 50))
}

func (p *Plugin) list(ctx context.Context, orgID uint, limit int) []MailLink {
	out := []MailLink{}
	if p.ctx.DB == nil || orgID == 0 {
		return out
	}
	p.ctx.DB.WithContext(ctx).Where("org_id = ?", orgID).
		Order("created_at DESC").Limit(limit).Find(&out)
	return out
}

// Compile-time assertions.
var (
	_ plugin.Plugin      = (*Plugin)(nil)
	_ plugin.Describer   = (*Plugin)(nil)
	_ plugin.MCPProvider = (*Plugin)(nil)
)
