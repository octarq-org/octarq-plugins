package dns

import (
	"context"
	"log"
)

func (p *Plugin) reconcileDomainHosts() {
	if p.db == nil || p.host == nil || p.host.Hosts() == nil {
		return
	}
	var domains []Domain
	if err := p.db.Find(&domains).Error; err != nil {
		log.Printf("dns: could not read domains to register their hosts: %v", err)
		return
	}
	for i := range domains {
		if err := p.syncDomainHosts(context.Background(), &domains[i]); err != nil {
			log.Printf("dns: could not register the host of %s: %v", domains[i].Name, err)
		}
	}
}
