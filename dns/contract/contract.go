package contract

import "context"

const ServiceManager = "dns.manager"

type Record struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Proxied  bool   `json:"proxied"`
	Comment  string `json:"comment"`
	Priority *int   `json:"priority,omitempty"`
}

type Manager interface {
	List(ctx context.Context, orgID, domainID uint) ([]Record, error)
	Set(ctx context.Context, orgID, domainID uint, r Record) (Record, error)
	Delete(ctx context.Context, orgID, domainID uint, recordID string) error
}
