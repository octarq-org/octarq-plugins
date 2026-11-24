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

func underBaseZone(db *gorm.DB, name string) bool {
	base := getBaseDomain(db, nil)
	if base == "" {
		return false
	}
	name = normalizeHost(name)
	return name == base || strings.HasSuffix(name, "."+base)
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

func domainHosts(dom *Domain) []string {
	if !dom.ForLink && !dom.ForMail {
		return []string{}
	}
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
		var enabledLink []string
		for _, h := range dom.LinkHosts {
			if h.Enabled {
				enabledLink = append(enabledLink, h.Host)
			}
		}
		if len(dom.LinkHosts) == 0 {
			addHost(dom.Name)
		} else {
			for _, h := range enabledLink {
				addHost(h)
			}
		}
	}
	if dom.ForMail {
		var enabledMail []string
		for _, h := range dom.MailHosts {
			if h.Enabled {
				enabledMail = append(enabledMail, h.Host)
			}
		}
		if len(dom.MailHosts) == 0 {
			addHost(dom.Name)
		} else {
			for _, h := range enabledMail {
				addHost(h)
			}
		}
	}
	return hosts
}

func (p *Plugin) syncDomainHosts(ctx context.Context, dom *Domain) error {
	if p.host == nil || p.host.Hosts() == nil {
		return nil
	}
	source := fmt.Sprintf("dns.domain.%d", dom.ID)
	hosts := domainHosts(dom)
	traffic := dom.ForLink
	err := p.host.Hosts().ReplaceOwned(ctx, dom.OrgID, source, hosts, traffic)
	if err != nil && !errors.Is(err, plugin.ErrHostsUnavailable) {
		return err
	}
	return nil
}

func (p *Plugin) removeDomainHosts(ctx context.Context, orgID, domainID uint) error {
	if p.host == nil || p.host.Hosts() == nil {
		return nil
	}
	source := fmt.Sprintf("dns.domain.%d", domainID)
	err := p.host.Hosts().ReplaceOwned(ctx, orgID, source, []string{}, false)
	if err != nil && !errors.Is(err, plugin.ErrHostsUnavailable) {
		return err
	}
	return nil
}
