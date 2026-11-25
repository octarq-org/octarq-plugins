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
			p.dropReservedDomain(dom)
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

func (p *Plugin) dropReservedDomain(dom *Domain) {
	if p.host != nil && p.host.Hosts() != nil {
		_ = p.removeDomainHosts(context.Background(), dom.OrgID, dom.ID)
	}
	p.db.Where("domain_id = ? AND owner_id = ?", dom.ID, dom.OrgID).Delete(&DDNSToken{})
	p.db.Where("id = ? AND owner_id = ?", dom.ID, dom.OrgID).Delete(&Domain{})
	forgetOrigin(dom.Name)
	log.Printf("dns: removed reserved platform domain %s from workspace %d", dom.Name, dom.OrgID)
}
