package contract

import "context"

const ServiceCreate = "links.create"

type Creator interface {
	CreateLink(ctx context.Context, orgID uint, targetURL string) (slug string, err error)
}
