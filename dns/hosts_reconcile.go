package dns

import (
	"context"
	"log"
)

func (p *Plugin) reconcileDomainHosts() {
	if p.db == nil {
		return
	}
	var domains []Domain
	if err := p.db.Find(&domains).Error; err != nil {
		log.Printf("dns: could not read domains to register their hosts: %v", err)
		return
	}
	for i := range domains {
		dom := &domains[i]
		if p.isReservedDomainName(dom.Name) {
			p.unbindReservedDomain(dom, p.isPlatformHostExact(dom.Name) || dom.ProviderAccountID != 0)
			continue
		}
		if p.host == nil || p.host.Hosts() == nil {
			continue
		}
		if err := p.syncDomainHosts(context.Background(), dom); err != nil {
			log.Printf("dns: could not register the host of %s: %v", dom.Name, err)
		}
	}
}

func (p *Plugin) unbindReservedDomain(dom *Domain, drop bool) {
	if p.host != nil && p.host.Hosts() != nil {
		if err := p.removeDomainHosts(context.Background(), dom.OrgID, dom.ID); err != nil {
			log.Printf("dns: could not unbind reserved host %s: %v", dom.Name, err)
		}
	}
	if !drop {
		return
	}
	if err := p.db.Where("domain_id = ? AND owner_id = ?", dom.ID, dom.OrgID).Delete(&DDNSToken{}).Error; err != nil {
		log.Printf("dns: could not delete ddns tokens for reserved host %s: %v", dom.Name, err)
		return
	}
	if err := p.db.Where("id = ? AND owner_id = ?", dom.ID, dom.OrgID).Delete(&Domain{}).Error; err != nil {
		log.Printf("dns: could not delete reserved host %s: %v", dom.Name, err)
		return
	}
	forgetOrigin(dom.Name)
	log.Printf("dns: removed reserved platform domain %s from workspace %d", dom.Name, dom.OrgID)
}
