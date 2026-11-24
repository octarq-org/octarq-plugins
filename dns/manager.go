package dns

import (
	"context"
	"errors"

	"github.com/octarq-org/octarq-plugins/dns/contract"
	"github.com/octarq-org/octarq-plugins/dns/provider"
)

type dnsManager struct{ p *Plugin }

func (p *Plugin) DNSManager() contract.Manager { return dnsManager{p} }

func (m dnsManager) resolve(orgID, domainID uint) (Domain, provider.Provider, error) {
	var dom Domain
	if orgID == 0 {
		return dom, nil, errOrgRequired
	}
	if err := m.p.db.Where("id = ? AND owner_id = ?", domainID, orgID).First(&dom).Error; err != nil {
		return dom, nil, err
	}
	prov, err := m.p.providerFor(dom)
	return dom, prov, err
}

var errOrgRequired = errors.New("dns: an org id is required to resolve a domain")

func (m dnsManager) List(ctx context.Context, orgID, domainID uint) ([]contract.Record, error) {
	dom, prov, err := m.resolve(orgID, domainID)
	if err != nil {
		return nil, err
	}
	recs, err := prov.ListRecords(ctx, dom.ZoneID)
	if err != nil {
		return nil, err
	}
	out := make([]contract.Record, len(recs))
	for i, r := range recs {
		out[i] = toContractRecord(r)
	}
	return out, nil
}

func (m dnsManager) Set(ctx context.Context, orgID, domainID uint, r contract.Record) (contract.Record, error) {
	dom, prov, err := m.resolve(orgID, domainID)
	if err != nil {
		return contract.Record{}, err
	}
	rec := fromContractRecord(r)
	var res provider.Record
	if r.ID == "" {
		res, err = prov.CreateRecord(ctx, dom.ZoneID, rec)
	} else {
		res, err = prov.UpdateRecord(ctx, dom.ZoneID, rec)
	}
	if err != nil {
		return contract.Record{}, err
	}
	return toContractRecord(res), nil
}

func (m dnsManager) Delete(ctx context.Context, orgID, domainID uint, recordID string) error {
	dom, prov, err := m.resolve(orgID, domainID)
	if err != nil {
		return err
	}
	return prov.DeleteRecord(ctx, dom.ZoneID, recordID)
}

func toContractRecord(r provider.Record) contract.Record {
	return contract.Record{
		ID: r.ID, Type: r.Type, Name: r.Name, Content: r.Content,
		TTL: r.TTL, Proxied: r.Proxied, Comment: r.Comment, Priority: r.Priority,
	}
}

func fromContractRecord(r contract.Record) provider.Record {
	return provider.Record{
		ID: r.ID, Type: r.Type, Name: r.Name, Content: r.Content,
		TTL: r.TTL, Proxied: r.Proxied, Comment: r.Comment, Priority: r.Priority,
	}
}
