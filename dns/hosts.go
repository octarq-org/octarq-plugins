package dns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/octarq-org/octarq-plugins/dns/provider"
	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")
	host = strings.TrimSuffix(host, ".")
	if i := strings.IndexAny(host, "/:"); i >= 0 {
		host = host[:i]
	}
	return host
}

func (p *Plugin) baseDomain() string {
	return getBaseDomain(p.db, p.host)
}

func getBaseDomain(db *gorm.DB, host plugin.Host) string {
	if host != nil && host.Settings() != nil {
		if v := host.Settings().GetGlobalSetting("base_domain"); v != "" {
			return normalizeHost(v)
		}
	}
	if db != nil && db.Migrator().HasTable("settings") {
		var row struct{ Value string }
		if err := db.Table("settings").Where("key = ?", "base_domain").Limit(1).Scan(&row).Error; err == nil && row.Value != "" {
			return normalizeHost(row.Value)
		}
	}
	return normalizeHost(os.Getenv("OCTARQ_BASE_DOMAIN"))
}

func reservedDomainName(name, base string, shared []string) bool {
	name = normalizeHost(name)
	if name == "" {
		return false
	}
	if base != "" && (name == base || strings.HasSuffix(name, "."+base)) {
		return true
	}
	for _, h := range shared {
		if name == h || strings.HasSuffix(name, "."+h) {
			return true
		}
	}
	return false
}

func (p *Plugin) isReservedDomainName(name string) bool {
	return reservedDomainName(name, p.baseDomain(), p.sharedPlatformHosts())
}

func (p *Plugin) sharedPlatformHosts() []string {
	raw := ""
	if p.host != nil && p.host.Settings() != nil {
		raw = p.host.Settings().GetGlobalSetting("shared_hosts")
	}
	if strings.TrimSpace(raw) == "" && p.db != nil && p.db.Migrator().HasTable("settings") {
		var row struct{ Value string }
		if err := p.db.Table("settings").Where("key = ?", "shared_hosts").Limit(1).Scan(&row).Error; err == nil {
			raw = row.Value
		}
	}
	if strings.TrimSpace(raw) == "" {
		raw = os.Getenv("OCTARQ_SHARED_HOSTS")
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	seen := make(map[string]bool)
	var out []string
	for _, f := range fields {
		h := normalizeHost(f)
		if h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

func underBaseZone(db *gorm.DB, name string) bool {
	return reservedDomainName(name, getBaseDomain(db, nil), nil)
}

func reservedInHostLists(db *gorm.DB, linkHosts, mailHosts hostlist.HostList) string {
	base := getBaseDomain(db, nil)
	if base == "" {
		return ""
	}
	for _, list := range []hostlist.HostList{linkHosts, mailHosts} {
		for _, h := range list {
			host := normalizeHost(h.Host)
			if host == base || strings.HasSuffix(host, "."+base) {
				return host
			}
		}
	}
	return ""
}

func hostsOutsideZone(name string, linkHosts, mailHosts hostlist.HostList) string {
	name = NormalizeHost(name)
	if name == "" {
		return ""
	}
	for _, list := range []hostlist.HostList{linkHosts, mailHosts} {
		for _, h := range list {
			host := NormalizeHost(h.Host)
			if host != name && !strings.HasSuffix(host, "."+name) {
				return host
			}
		}
	}
	return ""
}

type hostEntry struct {
	Host    string `json:"host"`
	Enabled *bool  `json:"enabled"`
}

func normalizeHosts(hosts []hostEntry) hostlist.HostList {
	seen := map[string]bool{}
	var out hostlist.HostList
	for _, h := range hosts {
		name := normalizeHost(h.Host)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		enabled := true
		if h.Enabled != nil {
			enabled = *h.Enabled
		}
		out = append(out, hostlist.Host{Host: name, Enabled: enabled})
	}
	return out
}

func (p *Plugin) ownsProviderAccount(r *http.Request, id uint) (bool, error) {
	if id == 0 {
		return false, nil
	}
	orgID := p.orgID(r)
	vs, vErr := p.vault()
	if vErr != nil {
		return false, fmt.Errorf("%w: %v", errProviderCredentials, vErr)
	}

	var acc ProviderAccount
	err := p.db.WithContext(r.Context()).Where("id = ? AND owner_id = ?", id, orgID).First(&acc).Error
	switch {
	case err == nil:
		return true, nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return false, fmt.Errorf("%w: %v", errProviderCredentials, err)
	}

	meta, metaErr := vs.GetMeta(r.Context(), orgID, id)
	if metaErr == nil {
		return meta != nil && meta.OrgID == orgID && meta.ProviderType == "dns", nil
	}
	if errors.Is(metaErr, plugin.ErrSecretNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("%w: %v", errProviderCredentials, metaErr)
}

type DNSProvidersInput struct {
	Ctx huma.Context `hidden:"true"`
}

func (i *DNSProvidersInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type DNSProvidersOutput struct {
	Body []string
}

func (p *Plugin) dnsProviders(ctx context.Context, input *DNSProvidersInput) (*DNSProvidersOutput, error) {
	return &DNSProvidersOutput{Body: provider.Names()}, nil
}

func domainHostList(dom *Domain) []string {
	seen := make(map[string]bool)
	var hosts []string
	addHost := func(raw string) {
		h := normalizeHost(raw)
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		hosts = append(hosts, h)
	}
	if dom.ForLink {
		for _, h := range dom.LinkHosts {
			if h.Enabled {
				addHost(h.Host)
			}
		}
	}
	if dom.ForMail {
		for _, h := range dom.MailHosts {
			if h.Enabled {
				addHost(h.Host)
			}
		}
	}
	return hosts
}

func domainApexTraffic(dom *Domain) bool {
	linkHosts := 0
	for _, h := range dom.LinkHosts {
		if h.Enabled {
			linkHosts++
		}
	}
	mailHosts := 0
	for _, h := range dom.MailHosts {
		if h.Enabled {
			mailHosts++
		}
	}
	return (dom.ForLink && linkHosts == 0) || (dom.ForMail && mailHosts == 0)
}

func (p *Plugin) syncDomainHosts(ctx context.Context, dom *Domain) error {
	if p.host == nil || p.host.Hosts() == nil {
		return nil
	}
	if err := p.replaceHosts(ctx, dom.OrgID, fmt.Sprintf("dns.domain.%d", dom.ID), domainHostList(dom), dom.ForLink); err != nil {
		return err
	}
	apex := normalizeHost(dom.Name)
	if apex == "" || apex == getBaseDomain(p.db, p.host) {
		return nil
	}
	return p.replaceHosts(ctx, dom.OrgID, fmt.Sprintf("dns.apex.%d", dom.ID), []string{apex}, domainApexTraffic(dom))
}

func (p *Plugin) replaceHosts(ctx context.Context, orgID uint, source string, hosts []string, traffic bool) error {
	err := p.host.Hosts().ReplaceOwned(ctx, orgID, source, hosts, traffic)
	if err != nil && !errors.Is(err, plugin.ErrHostsUnavailable) {
		return err
	}
	return nil
}

func (p *Plugin) removeDomainHosts(ctx context.Context, orgID, domainID uint) error {
	if p.host == nil || p.host.Hosts() == nil {
		return nil
	}
	if err := p.replaceHosts(ctx, orgID, fmt.Sprintf("dns.domain.%d", domainID), []string{}, false); err != nil {
		return err
	}
	return p.replaceHosts(ctx, orgID, fmt.Sprintf("dns.apex.%d", domainID), []string{}, false)
}
