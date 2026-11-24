package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"github.com/octarq-org/octarq/server/plugin"
)

var (
	mcpResourcesMu sync.Mutex
	mcpResources   = make(map[string]string)
)

func hasResourceOnServer(srv *mcp.Server, uri string) bool {
	if srv == nil {
		return false
	}
	v := reflect.ValueOf(srv)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return false
	}
	rf := v.FieldByName("resources")
	if !rf.IsValid() {
		return false
	}
	if rf.Kind() == reflect.Ptr {
		rf = rf.Elem()
	}
	if !rf.IsValid() || rf.Kind() != reflect.Struct {
		return false
	}
	ff := rf.FieldByName("features")
	if !ff.IsValid() || ff.Kind() != reflect.Map {
		return false
	}
	return ff.MapIndex(reflect.ValueOf(uri)).IsValid()
}

func addMCPResource(srv *mcp.Server, owner string, r *mcp.Resource, h mcp.ResourceHandler) {
	mcpResourcesMu.Lock()
	defer mcpResourcesMu.Unlock()

	if existing, exists := mcpResources[r.URI]; exists && existing != owner {
		panic(fmt.Sprintf("mcp: resource conflict: %q already registered by %q", r.URI, existing))
	}
	if hasResourceOnServer(srv, r.URI) && mcpResources[r.URI] != owner {
		panic(fmt.Sprintf("mcp: resource conflict: %q already registered on server", r.URI))
	}
	mcpResources[r.URI] = owner
	srv.AddResource(r, h)
}

type DomainsHealthResource struct {
	OrgID             uint                 `json:"org_id"`
	TotalDomains      int                  `json:"total_domains"`
	ActiveMailDomains int                  `json:"active_mail_domains"`
	ActiveLinkDomains int                  `json:"active_link_domains"`
	Domains           []DomainHealthDetail `json:"domains"`
	GeneratedAt       time.Time            `json:"generated_at"`
}

type DomainHealthDetail struct {
	ID                uint              `json:"id"`
	Name              string            `json:"name"`
	Status            string            `json:"status"`
	ForMail           bool              `json:"for_mail"`
	ForLink           bool              `json:"for_link"`
	ProviderAccountID uint              `json:"provider_account_id"`
	ZoneID            string            `json:"zone_id,omitempty"`
	LinkHosts         hostlist.HostList `json:"link_hosts,omitempty"`
	MailHosts         hostlist.HostList `json:"mail_hosts,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

func (p *Plugin) registerMCPResources(srv *mcp.Server) {
	addMCPResource(srv, "dns", &mcp.Resource{
		URI:         "octarq://domains/health",
		Name:        "domains_health",
		Title:       "Domains Health",
		Description: "Read-only DNS, routing capability, and health status for domains managed in the current workspace.",
		MIMEType:    "application/json",
	}, p.mcpResourceDomainsHealth)
}

func (p *Plugin) mcpResourceDomainsHealth(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if req == nil || req.Params == nil {
		return nil, errors.New("missing request params")
	}
	if req.Params.URI != "octarq://domains/health" {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	orgID := plugin.OrgIDFromContext(ctx)
	if orgID == 0 {
		return nil, errNoOrgInContext
	}
	if p.db == nil {
		return nil, errors.New("dns database unavailable")
	}
	var doms []Domain
	if err := p.db.WithContext(ctx).
		Where("owner_id = ?", orgID).
		Order("name ASC").
		Find(&doms).Error; err != nil {
		return nil, err
	}
	details := make([]DomainHealthDetail, 0, len(doms))
	var activeMail, activeLink int
	for _, d := range doms {
		status := "inactive"
		if d.ForMail || d.ForLink {
			status = "active"
		}
		if d.ForMail {
			activeMail++
		}
		if d.ForLink {
			activeLink++
		}
		details = append(details, DomainHealthDetail{
			ID:                d.ID,
			Name:              d.Name,
			Status:            status,
			ForMail:           d.ForMail,
			ForLink:           d.ForLink,
			ProviderAccountID: d.ProviderAccountID,
			ZoneID:            d.ZoneID,
			LinkHosts:         d.LinkHosts,
			MailHosts:         d.MailHosts,
			CreatedAt:         d.CreatedAt,
			UpdatedAt:         d.UpdatedAt,
		})
	}
	res := DomainsHealthResource{
		OrgID:             orgID,
		TotalDomains:      len(doms),
		ActiveMailDomains: activeMail,
		ActiveLinkDomains: activeLink,
		Domains:           details,
		GeneratedAt:       time.Now().UTC(),
	}
	buf, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(buf),
			},
		},
	}, nil
}
