package mail

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

type Plugin struct {
	db                  *gorm.DB
	orgID               func(*http.Request) uint
	audit               func(r *http.Request, action, targetType string, targetID uint, meta map[string]any)
	getWorkspaceSetting func(orgID uint, key string) string
	setWorkspaceSetting func(orgID uint, key, value string) error
	getGlobalSetting    func(key string) string
	setGlobalSetting    func(key, value string) error
	sendLimiter         *rateLimiter
	testLimiter         *rateLimiter
	emailMu             sync.RWMutex
	emailHandlers       []func(plugin.EmailEvent)
	notify              func(ctx context.Context, kind, cfgJSON, message string) error
	emit                func(ctx context.Context, payload plugin.NotificationPayload) error
	emitTo              func(ctx context.Context, recipient plugin.NotificationRecipient, payload plugin.NotificationPayload) error
	publishEvent        func(orgID uint, event string, data any)
	recordUsage         func(orgID uint, metric string, n int64)
	requireRole         func(r *http.Request, min string) bool
	ctx                 *plugin.Context
	host                plugin.Host
	vaultSvc            plugin.VaultService
}

func (p *Plugin) Host() plugin.Host { return p.host }

func (p *Plugin) SetHost(h plugin.Host) { p.host = h }

var (
	_ plugin.Plugin       = (*Plugin)(nil)
	_ plugin.Describer    = (*Plugin)(nil)
	_ plugin.MenuProvider = (*Plugin)(nil)
	_ plugin.HelpDocsFS   = (*Plugin)(nil)
)

var (
	_ plugin.MailSender       = (*Plugin)(nil).sendMail
	_ plugin.EmailDispatcher  = (*Plugin)(nil).OnEmail
	_ plugin.MailReady        = (*Plugin)(nil).mailReady
	_ plugin.SystemMailSender = (*Plugin)(nil).sendSystemMail
	_ plugin.ExportFunc       = (*Plugin)(nil).exportData
	_ plugin.PurgeFunc        = (*Plugin)(nil).purge
	_ plugin.OverviewFunc     = (*Plugin)(nil).overview
	_ plugin.MCPExporter      = (*Plugin)(nil).mcpExportEmails
	_ plugin.MCPExporter      = (*Plugin)(nil).mcpExportMailboxes
)

func New() *Plugin {
	return &Plugin{
		sendLimiter: newRateLimiter("", "send", 100, time.Hour),
		testLimiter: newRateLimiter("", "smtp_test", 10, time.Minute),
	}
}

func (p *Plugin) Name() string { return "mail" }

func (p *Plugin) Describe() plugin.Info {
	return plugin.Info{
		Title:            "Mail",
		Description:      "Transactional email sending, mailbox receiving, and SMTP configurations.",
		Category:         plugin.CategoryMessaging,
		Tags:             []string{"smtp", "inbox", "webhooks"},
		EnabledByDefault: true,
		Requires:         []string{"dns", "links"},
	}
}

func (p *Plugin) Models() []any {
	return []any{&Mailbox{}, &Email{}, &SMTPSender{}, &MailRawBlob{}, &MailSuppression{}, &MailContact{}, &MailDraft{}}
}

func (p *Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{ID: "mail", Label: "Mail", Path: "/mail", Icon: "mail", Category: "Messaging", Area: "operations", Order: 20},
	}
}

func (p *Plugin) Actions() []plugin.Action {
	return []plugin.Action{
		{ID: "create-mailbox", Label: "New Mailbox", Path: "/mail?create=1", Icon: "mail", Category: "Messaging", Order: 10},
	}
}

//go:embed docs
var docs embed.FS

func (p *Plugin) HelpDocsFS() fs.FS { return docs }

func (p *Plugin) tenantDB(orgID uint) *plugin.TenantDB {
	if p.ctx != nil && p.ctx.TenantDB != nil {
		if tdb := p.ctx.TenantDB(orgID); tdb != nil {
			return tdb
		}
	}
	if p.db != nil && orgID != 0 {
		tdb, _ := plugin.NewTenantDB(p.db, orgID)
		return tdb
	}
	return nil
}

func (p *Plugin) orgDB(r *http.Request) *plugin.TenantDB {
	return p.tenantDB(p.orgID(r))
}

func (p *Plugin) Mount(mux plugin.Mux, ctx *plugin.Context) {
	p.ctx = ctx
	if ctx.DB != nil {
		p.db = ctx.DB
	}
	p.host = plugin.EnsureHost(ctx)
	if p.host != nil {
		if p.host.Session() != nil {
			p.orgID = p.host.Session().OrgID
			p.requireRole = p.host.Session().RequireRole
		}
		if p.host.Settings() != nil {
			p.getWorkspaceSetting = p.host.Settings().GetWorkspaceSetting
			p.setWorkspaceSetting = p.host.Settings().SetWorkspaceSetting
			p.getGlobalSetting = p.host.Settings().GetGlobalSetting
			p.setGlobalSetting = p.host.Settings().SetGlobalSetting
		}
		if p.host.Events() != nil {
			p.publishEvent = p.host.Events().PublishEvent
			p.host.Events().RegisterWebhookEvent(plugin.WebhookEventDef{Key: "email.receive", Group: "Email", Title: "Email Received", Description: "An inbound email was delivered to a mailbox"})
			p.host.Events().RegisterWebhookEvent(plugin.WebhookEventDef{Key: "email.send_failed", Group: "Email", Title: "Email Send Failed", Description: "An outbound email failed to send through the configured SMTP sender"})
		}
	}
	if ctx.Audit != nil {
		p.audit = ctx.Audit
	}
	if ctx.Notify != nil {
		p.notify = ctx.Notify
	}
	if ctx.Emit != nil {
		p.emit = ctx.Emit
	}
	if ctx.EmitTo != nil {
		p.emitTo = ctx.EmitTo
	}
	if ctx.RecordUsage != nil {
		p.recordUsage = ctx.RecordUsage
	}

	RegisterViews(ctx)
	p.registerDeclarativeEndpoints(ctx)

	api := ctx.Huma
	if api != nil {
		registerAPIRoutes(p, api, ctx)
	}

	if ctx.Provide != nil {
		ctx.Provide(plugin.ServiceMailDispatcher, plugin.EmailDispatcher(p.OnEmail))
		ctx.Provide(plugin.OverviewServiceName("mail"), plugin.OverviewFunc(p.overview))
		ctx.Provide(plugin.PurgeServiceName("mail"), plugin.PurgeFunc(p.purge))
		ctx.Provide(plugin.ExportServiceName("mail"), plugin.ExportFunc(p.exportData))
		ctx.Provide(plugin.ServiceMailSend, plugin.MailSender(p.sendMail))
		ctx.Provide(plugin.ServiceMailReady, plugin.MailReady(p.mailReady))
		ctx.Provide(plugin.ServiceMailSendSystem, plugin.SystemMailSender(p.sendSystemMail))
		ctx.Provide(plugin.MCPExportServiceName("mailboxes"), plugin.MCPExporter(p.mcpExportMailboxes))
		ctx.Provide(plugin.MCPExportServiceName("emails"), plugin.MCPExporter(p.mcpExportEmails))
	}
}
