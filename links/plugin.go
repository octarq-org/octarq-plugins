package links

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

type Plugin struct {
	db     *gorm.DB
	engine *Engine
	auth   struct {
		UserID func(r *http.Request) uint
		OrgID  func(r *http.Request) uint
	}
	audit               func(r *http.Request, action, targetType string, targetID uint, meta map[string]any)
	getGlobalSetting    func(key string) string
	getWorkspaceSetting func(orgID uint, key string) string
	enqueue             func(ctx context.Context, taskType string, payload []byte) error
	deleteCache         func(ctx context.Context, key string) error
	publishEvent        func(orgID uint, event string, data any)
	requireRole         func(r *http.Request, min string) bool
	isInstanceAdmin     func(r *http.Request) bool
	ctx                 *plugin.Context
	host                plugin.Host
}

func (p *Plugin) Host() plugin.Host { return p.host }

func (p *Plugin) SetHost(h plugin.Host) { p.host = h }

var (
	_ plugin.Plugin       = (*Plugin)(nil)
	_ plugin.Describer    = (*Plugin)(nil)
	_ plugin.MenuProvider = (*Plugin)(nil)
	_ plugin.HelpDocsFS   = (*Plugin)(nil)
	_ plugin.Starter      = (*Plugin)(nil)
)

var (
	_ plugin.ExportFunc   = (*Plugin)(nil).exportData
	_ plugin.PurgeFunc    = (*Plugin)(nil).purge
	_ plugin.OverviewFunc = (*Plugin)(nil).overview
	_ plugin.LinkResolver = (*Plugin)(nil).resolveSlug
	_ plugin.MCPExporter  = (*Plugin)(nil).mcpExportLinks
	_ plugin.CleanupFunc  = (*Plugin)(nil).cleanupEvents
)

func New() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Name() string { return "links" }
func (p *Plugin) Describe() plugin.Info {
	return plugin.Info{
		Title:            "Short Links",
		Description:      "Short link creation, custom domain routing, and click analytics.",
		Category:         plugin.CategoryMarketing,
		Tags:             []string{"url", "analytics", "routing"},
		EnabledByDefault: true,
		Requires:         []string{"dns"},
	}
}
func (p *Plugin) Models() []any {
	return []any{&Link{}, &LinkEvent{}}
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

func (p *Plugin) orgDB(r *http.Request) *gorm.DB {
	orgID := p.orgID(r)
	if tdb := p.tenantDB(orgID); tdb != nil {
		return tdb.Scoped(&Link{})
	}
	if p.db != nil {
		return p.db.Where("owner_id = ?", orgID)
	}
	return nil
}

func (p *Plugin) orgID(r *http.Request) uint {
	return p.auth.OrgID(r)
}

var builtinReservedSlugs = map[string]bool{
	"admin":      true,
	"api":        true,
	"assets":     true,
	"portal":     true,
	"robots.txt": true,
}

func (p *Plugin) isReservedSlug(slug string) bool {
	slug = strings.ToLower(slug)
	if builtinReservedSlugs[slug] {
		return true
	}
	if p.getGlobalSetting != nil {
		for _, res := range splitList(p.getGlobalSetting("reserved_slugs")) {
			if res == slug {
				return true
			}
		}
	}
	return false
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (p *Plugin) hasRole(r *http.Request, min string) bool {
	if p.requireRole == nil {
		return false
	}
	return p.requireRole(r, min)
}
