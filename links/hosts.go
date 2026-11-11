package links

import (
	"os"

	"github.com/octarq-org/octarq/server/pkg/hostlist"
)

type domainLinkRow struct {
	Name      string            `gorm:"column:name"`
	LinkHosts hostlist.HostList `gorm:"column:link_hosts;type:text"`
}

func normalizeHost(host string) string { return hostlist.NormalizeHost(host) }

// ownsHost reports whether orgID may serve short links on host.
func (p *Plugin) ownsHost(orgID uint, host string) bool {
	normHost := normalizeHost(host)
	if normHost == "" {
		return true
	}
	if orgID == 0 || p.db == nil || !p.db.Migrator().HasTable("domains") {
		return false
	}
	tdb := p.tenantDB(orgID)
	if tdb == nil {
		return false
	}
	var doms []domainLinkRow
	if err := tdb.Table("domains").Where("for_link = ?", true).Find(&doms).Error; err != nil {
		return false
	}
	for _, d := range doms {
		hosts := d.LinkHosts.Enabled()
		if len(hosts) == 0 {
			if normalizeHost(d.Name) == normHost {
				return true
			}
		} else {
			for _, h := range hosts {
				if normalizeHost(h) == normHost {
					return true
				}
			}
		}
	}
	return false
}

// linkHostRequired reports whether hostless (host = "") links must be refused
// for orgID: a shared base domain is configured AND the org has at least one
// link host to pick from. There the empty host means the cross-tenant shared
// namespace — tenant A's slug silently blocks tenant B, and the 409 acts as an
// existence probe across orgs. A self-hosted single-tenant install (no base
// domain) keeps its neutral-host links, which scopeForHost still serves.
func (p *Plugin) linkHostRequired(orgID uint) bool {
	if orgID == 0 || p.db == nil || !p.db.Migrator().HasTable("domains") {
		return false
	}
	if p.baseDomain() == "" {
		return false
	}
	tdb := p.tenantDB(orgID)
	if tdb == nil {
		return false
	}
	var n int64
	tdb.Table("domains").Where("for_link = ?", true).Count(&n)
	return n > 0
}

func (p *Plugin) baseDomain() string {
	if p.getGlobalSetting != nil {
		if v := p.getGlobalSetting("base_domain"); v != "" {
			return hostlist.NormalizeHost(v)
		}
	}
	if p.host != nil && p.host.Settings() != nil {
		if v := p.host.Settings().GetGlobalSetting("base_domain"); v != "" {
			return hostlist.NormalizeHost(v)
		}
	}
	if p.db != nil && p.db.Migrator().HasTable("settings") {
		var val string
		if err := p.db.Table("settings").Where("key = ?", "base_domain").Pluck("value", &val).Error; err == nil && val != "" {
			return hostlist.NormalizeHost(val)
		}
	}
	return hostlist.NormalizeHost(os.Getenv("OCTARQ_BASE_DOMAIN"))
}
