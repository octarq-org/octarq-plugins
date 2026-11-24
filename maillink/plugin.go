package maillink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	linkscontract "github.com/octarq-org/octarq-plugins/links/contract"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

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
		p.host.Events().OnEvent(p.onEvent)
	}
	if mux == nil || ctx.Guard == nil {
		return
	}
	mux.Handle("GET /api/maillink/recent", ctx.Guard(http.HandlerFunc(p.recent)))
}

func (p *Plugin) Validate(ctx context.Context, host plugin.Host) error {
	if host == nil {
		return errors.New("maillink: host SPI is required")
	}
	return nil
}

type inboundEmail struct {
	OrgID   uint   `json:"orgId"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}

func (p *Plugin) onEvent(topic string, payload json.RawMessage) {
	if topic != "mail.inbound" {
		return
	}
	var e inboundEmail
	if err := json.Unmarshal(payload, &e); err != nil {
		return
	}
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
	creator, ok := plugin.LookupAs[linkscontract.Creator](p.ctx, linkscontract.ServiceCreate)
	if !ok {
		return
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

func (*Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{ID: "maillink", Label: "Mail Links", Path: "/maillink", Icon: "link-2", Category: "Marketing"},
	}
}

var (
	_ plugin.Plugin       = (*Plugin)(nil)
	_ plugin.Validator    = (*Plugin)(nil)
	_ plugin.Describer    = (*Plugin)(nil)
	_ plugin.MCPProvider  = (*Plugin)(nil)
	_ plugin.MenuProvider = (*Plugin)(nil)
)
