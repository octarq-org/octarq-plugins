package dns

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/octarq-org/octarq-plugins/dns/provider"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

const ServiceDNSManager = plugin.ServiceDNSManager

var (
	errNotFound                = errors.New("not found")
	errProviderAccountNotFound = errors.New("provider account not found")
	errProviderCredentials     = errors.New("provider credentials unavailable")
)

type Plugin struct {
	db          *gorm.DB
	orgID       func(*http.Request) uint
	audit       func(r *http.Request, action, targetType string, targetID uint, meta map[string]any)
	requireRole func(r *http.Request, min string) bool

	publishEvent func(orgID uint, event string, data any)
	ctx          *plugin.Context
	host         plugin.Host
	vaultSvc     plugin.VaultService

	lookupTXT   func(string) ([]string, error)
	lookupCNAME func(string) (string, error)
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
	_ plugin.ExportFunc   = (*Plugin)(nil).exportData
	_ plugin.PurgeFunc    = (*Plugin)(nil).purge
	_ plugin.OverviewFunc = (*Plugin)(nil).overview
	_ plugin.MCPExporter  = (*Plugin)(nil).mcpExportDomains
)

func New() *Plugin {
	return &Plugin{
		lookupTXT:   net.LookupTXT,
		lookupCNAME: net.LookupCNAME,
	}
}

func (p *Plugin) Name() string { return "dns" }

func (p *Plugin) Describe() plugin.Info {
	return plugin.Info{
		Title:            "Domains & DNS",
		Description:      "Domain lifecycle and DNS management across Cloudflare and DNSPod.",
		Category:         plugin.CategoryInfrastructure,
		Tags:             []string{"dns", "cloudflare", "dnspod"},
		EnabledByDefault: true,
	}
}

func (p *Plugin) Models() []any {
	return []any{&Domain{}, &ProviderAccount{}, &DDNSToken{}}
}

func (p *Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{ID: "domains", Label: "DNS", Path: "/domains", Icon: "globe", Category: "Network", Area: "assets", Order: 10},
	}
}

func (p *Plugin) Actions() []plugin.Action {
	return []plugin.Action{
		{ID: "create-domain", Label: "Add Domain", Path: "/domains?create=1", Icon: "globe", Category: "Network", Order: 10},
	}
}

//go:embed docs
var docs embed.FS

func (p *Plugin) HelpDocsFS() fs.FS { return docs }

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
		if p.host.Events() != nil {
			p.publishEvent = p.host.Events().PublishEvent
			p.host.Events().RegisterWebhookEvent(plugin.WebhookEventDef{Key: "domain.create", Group: "Domain", Title: "Domain Created", Description: "A domain was added to the workspace"})
			p.host.Events().RegisterWebhookEvent(plugin.WebhookEventDef{Key: "domain.verify_failed", Group: "Domain", Title: "Domain Verification Failed", Description: "A domain's provider or DNS verification check failed"})
		}
	}
	if ctx.Audit != nil {
		p.audit = ctx.Audit
	}

	api := ctx.Huma
	if api != nil {
		huma.Register(api, huma.Operation{Method: "GET", Path: "/api/dns/providers", Summary: "DNS Providers", Tags: []string{"DNS"}}, p.dnsProviders)

		huma.Register(api, huma.Operation{Method: "GET", Path: "/api/provider-accounts", Summary: "List Provider Accounts", Tags: []string{"Providers"}}, p.listProviderAccounts)
		huma.Register(api, huma.Operation{Method: "POST", Path: "/api/provider-accounts", Summary: "Create Provider Account", Tags: []string{"Providers"}, DefaultStatus: 201}, p.createProviderAccount)
		huma.Register(api, huma.Operation{Method: "PUT", Path: "/api/provider-accounts/{id}", Summary: "Update Provider Account", Tags: []string{"Providers"}}, p.updateProviderAccount)
		huma.Register(api, huma.Operation{Method: "DELETE", Path: "/api/provider-accounts/{id}", Summary: "Delete Provider Account", Tags: []string{"Providers"}}, p.deleteProviderAccount)

		huma.Register(api, huma.Operation{Method: "POST", Path: "/api/domains/sync", Summary: "Sync Domains", Tags: []string{"Domains"}}, p.syncDomains)
		huma.Register(api, huma.Operation{Method: "GET", Path: "/api/domains", Summary: "List Domains", Tags: []string{"Domains"}}, p.listDomains)
		huma.Register(api, huma.Operation{Method: "POST", Path: "/api/domains", Summary: "Create Domain", Tags: []string{"Domains"}, DefaultStatus: 201}, p.createDomain)
		huma.Register(api, huma.Operation{Method: "PUT", Path: "/api/domains/{id}", Summary: "Update Domain", Tags: []string{"Domains"}}, p.updateDomain)
		huma.Register(api, huma.Operation{Method: "DELETE", Path: "/api/domains/{id}", Summary: "Delete Domain", Tags: []string{"Domains"}}, p.deleteDomain)
		huma.Register(api, huma.Operation{Method: "GET", Path: "/api/domains/{id}/verify-dns", Summary: "Verify Domain DNS", Tags: []string{"Domains"}}, p.verifyDomainDNS)
		huma.Register(api, huma.Operation{Method: "GET", Path: "/api/domains/{id}/records", Summary: "List DNS Records", Tags: []string{"Domains"}}, p.listRecords)
		huma.Register(api, huma.Operation{Method: "POST", Path: "/api/domains/{id}/records", Summary: "Create DNS Record", Tags: []string{"Domains"}, DefaultStatus: 201}, p.createRecord)
		huma.Register(api, huma.Operation{Method: "PUT", Path: "/api/domains/{id}/records/{rid}", Summary: "Update DNS Record", Tags: []string{"Domains"}}, p.updateRecord)
		huma.Register(api, huma.Operation{Method: "DELETE", Path: "/api/domains/{id}/records/{rid}", Summary: "Delete DNS Record", Tags: []string{"Domains"}}, p.deleteRecord)
		huma.Register(api, huma.Operation{Method: "GET", Path: "/api/domains/{id}/email-blueprint", Summary: "Email DNS Blueprint", Tags: []string{"Domains"}}, p.emailBlueprint)
		huma.Register(api, huma.Operation{Method: "POST", Path: "/api/domains/{id}/apply-email-blueprint", Summary: "Apply Email DNS Blueprint", Tags: []string{"Domains"}, DefaultStatus: 200}, p.applyEmailBlueprint)

		huma.Register(api, huma.Operation{Method: "GET", Path: "/api/dns/ddns", Summary: "List DDNS Tokens", Tags: []string{"DDNS"}}, p.listDDNSTokens)
		huma.Register(api, huma.Operation{Method: "POST", Path: "/api/dns/ddns", Summary: "Create DDNS Token", Tags: []string{"DDNS"}, DefaultStatus: 201}, p.createDDNSToken)
		huma.Register(api, huma.Operation{Method: "DELETE", Path: "/api/dns/ddns/{id}", Summary: "Delete DDNS Token", Tags: []string{"DDNS"}}, p.deleteDDNSToken)

		huma.Register(api, huma.Operation{
			Method:   "GET",
			Path:     "/api/dns/ddns/update",
			Summary:  "DDNS Update (GET)",
			Tags:     []string{"DDNS"},
			Metadata: map[string]any{"public": true},
		}, p.updateDDNS)
		huma.Register(api, huma.Operation{
			Method:   "POST",
			Path:     "/api/dns/ddns/update",
			Summary:  "DDNS Update (POST)",
			Tags:     []string{"DDNS"},
			Metadata: map[string]any{"public": true},
		}, p.updateDDNS)
	}

	ctx.Provide(plugin.ServiceDNSManager, p.DNSManager())
	ctx.Provide(plugin.OverviewServiceName("dns"), plugin.OverviewFunc(p.overview))
	ctx.Provide(plugin.PurgeServiceName("dns"), plugin.PurgeFunc(p.purge))
	ctx.Provide(plugin.ExportServiceName("dns"), plugin.ExportFunc(p.exportData))
	ctx.Provide(plugin.MCPExportServiceName("domains"), plugin.MCPExporter(p.mcpExportDomains))
	ctx.Provide("dns.trust_proxy", SetTrustProxy)
}

func (p *Plugin) purge(orgID uint) error {
	var names []string
	p.db.Model(&Domain{}).Where("owner_id = ?", orgID).Pluck("name", &names)
	p.db.Where("owner_id = ?", orgID).Delete(&Domain{})
	forgetOrigin(names...)

	var accounts []ProviderAccount
	p.db.Where("owner_id = ? AND vault_item_id > 0", orgID).Find(&accounts)
	if vs, err := p.vault(); err == nil {
		for _, acc := range accounts {
			_ = vs.DeleteSecret(context.Background(), orgID, acc.VaultItemID)
		}
	}

	p.db.Where("owner_id = ?", orgID).Delete(&ProviderAccount{})
	p.db.Where("owner_id = ?", orgID).Delete(&DDNSToken{})
	return nil
}

func (p *Plugin) exportData(orgID uint) map[string]any {
	var doms []Domain
	var provs []ProviderAccount
	p.db.Where("owner_id = ?", orgID).Find(&doms)
	p.db.Where("owner_id = ?", orgID).Find(&provs)
	return map[string]any{
		"domains":          doms,
		"providerAccounts": provs,
	}
}

func (p *Plugin) overview(orgID uint, includeBot bool) map[string]any {
	count := func(model any, conds ...any) int64 {
		var n int64
		q := p.db.Model(model).Where("owner_id = ?", orgID)
		if len(conds) > 0 {
			q = q.Where(conds[0], conds[1:]...)
		}
		q.Count(&n)
		return n
	}
	return map[string]any{
		"domains":     count(&Domain{}),
		"linkDomains": count(&Domain{}, "for_link = ?", true),
		"mailDomains": count(&Domain{}, "for_mail = ?", true),
	}
}

func (p *Plugin) orgDB(r *http.Request) *gorm.DB {
	return p.db.Where("owner_id = ?", p.orgID(r))
}

func (p *Plugin) vault() (plugin.VaultService, error) {
	if p.vaultSvc != nil {
		return p.vaultSvc, nil
	}
	if svc, ok := plugin.LookupAs[plugin.VaultService](p.ctx, plugin.ServiceVault); ok && svc != nil {
		return svc, nil
	}
	return nil, plugin.ErrVaultUnavailable
}

func (p *Plugin) providerFor(dom Domain) (provider.Provider, error) {
	return p.providerForContext(context.Background(), dom)
}

func (p *Plugin) providerForContext(ctx context.Context, dom Domain) (provider.Provider, error) {
	if dom.ProviderAccountID == 0 {
		return nil, errors.New("domain has no provider account configured")
	}

	vs, err := p.vault()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errProviderCredentials, err)
	}

	var acc ProviderAccount
	err = p.db.WithContext(ctx).Where("id = ? AND owner_id = ?", dom.ProviderAccountID, dom.OrgID).First(&acc).Error
	switch {
	case err == nil:
		if acc.VaultItemID == 0 {
			return nil, fmt.Errorf("%w: provider account %d has no credentials", errProviderCredentials, acc.ID)
		}
		sec, secErr := vs.GetSecret(ctx, dom.OrgID, acc.VaultItemID)
		if secErr != nil {
			if errors.Is(secErr, plugin.ErrSecretNotFound) {
				return nil, errProviderAccountNotFound
			}
			return nil, fmt.Errorf("%w: %v", errProviderCredentials, secErr)
		}
		if sec == nil || len(sec.Payload) == 0 {
			return nil, fmt.Errorf("%w: provider account %d has no credentials", errProviderCredentials, acc.ID)
		}
		cfgJSON, mErr := json.Marshal(sec.Payload)
		if mErr != nil {
			return nil, fmt.Errorf("%w: %v", errProviderCredentials, mErr)
		}
		return provider.New(acc.Type, cfgJSON)
	case errors.Is(err, gorm.ErrRecordNotFound):
		sec, secErr := vs.GetSecret(ctx, dom.OrgID, dom.ProviderAccountID)
		if secErr != nil {
			if errors.Is(secErr, plugin.ErrSecretNotFound) {
				return nil, errProviderAccountNotFound
			}
			return nil, fmt.Errorf("%w: %v", errProviderCredentials, secErr)
		}
		if sec == nil || sec.Meta.OrgID != dom.OrgID || sec.Meta.ProviderType != "dns" {
			return nil, errProviderAccountNotFound
		}
		if len(sec.Payload) == 0 {
			return nil, fmt.Errorf("%w: integration %d has no credentials", errProviderCredentials, dom.ProviderAccountID)
		}
		cfgJSON, mErr := json.Marshal(sec.Payload)
		if mErr != nil {
			return nil, fmt.Errorf("%w: %v", errProviderCredentials, mErr)
		}
		return provider.New(sec.Meta.Provider, cfgJSON)
	default:
		return nil, fmt.Errorf("%w: %v", errProviderCredentials, err)
	}
}

func (p *Plugin) hasRole(r *http.Request, min string) bool {
	if p.requireRole == nil {
		return false
	}
	return p.requireRole(r, min)
}
