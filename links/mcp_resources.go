package links

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

type TrendingLinksResource struct {
	OrgID       uint            `json:"org_id"`
	Links       []LinkMetricOut `json:"links"`
	GeneratedAt time.Time       `json:"generated_at"`
}

type TrafficMetricsResource struct {
	OrgID       uint            `json:"org_id"`
	TotalLinks  int64           `json:"total_links"`
	ActiveLinks int64           `json:"active_links"`
	TotalClicks int64           `json:"total_clicks"`
	Clicks7d    int64           `json:"clicks_7d"`
	Clicks30d   int64           `json:"clicks_30d"`
	TopLinks    []LinkMetricOut `json:"top_links"`
	GeneratedAt time.Time       `json:"generated_at"`
}

type LinkMetricOut struct {
	ID     uint   `json:"id"`
	Host   string `json:"host"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Clicks int64  `json:"clicks"`
}

func (p *Plugin) registerMCPResources(srv *mcp.Server) {
	addMCPResource(srv, "links", &mcp.Resource{
		URI:         "octarq://links/trending",
		Name:        "links_trending",
		Title:       "Trending Links",
		Description: "Short links of the current workspace ranked by clicks.",
		MIMEType:    "application/json",
	}, p.mcpResourceTrending)

	addMCPResource(srv, "links", &mcp.Resource{
		URI:         "octarq://metrics/traffic",
		Name:        "traffic_metrics",
		Title:       "Traffic Metrics",
		Description: "Read-only traffic and short-link redirection analytics for the current workspace.",
		MIMEType:    "application/json",
	}, p.mcpResourceTraffic)
}

func (p *Plugin) mcpResourceTrending(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if req == nil || req.Params == nil {
		return nil, errors.New("missing request params")
	}
	if req.Params.URI != "octarq://links/trending" {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	orgID := plugin.OrgIDFromContext(ctx)
	if orgID == 0 {
		return nil, errNoOrgInContext
	}
	if p.db == nil {
		return nil, errors.New("links database unavailable")
	}
	links := make([]LinkMetricOut, 0)
	err := p.db.WithContext(ctx).Model(&Link{}).
		Select("id, host, slug, title, clicks").
		Where("owner_id = ? AND archived = ?", orgID, false).
		Order("clicks DESC, id ASC").
		Limit(10).
		Scan(&links).Error
	if err != nil {
		return nil, err
	}
	res := TrendingLinksResource{
		OrgID:       orgID,
		Links:       links,
		GeneratedAt: time.Now().UTC(),
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

func (p *Plugin) mcpResourceTraffic(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if req == nil || req.Params == nil {
		return nil, errors.New("missing request params")
	}
	if req.Params.URI != "octarq://metrics/traffic" {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	orgID := plugin.OrgIDFromContext(ctx)
	if orgID == 0 {
		return nil, errNoOrgInContext
	}
	if p.db == nil {
		return nil, errors.New("links database unavailable")
	}
	var totalLinks int64
	if err := p.db.WithContext(ctx).Model(&Link{}).Where("owner_id = ?", orgID).Count(&totalLinks).Error; err != nil {
		return nil, err
	}
	var activeLinks int64
	if err := p.db.WithContext(ctx).Model(&Link{}).Where("owner_id = ? AND archived = ? AND enabled = ?", orgID, false, true).Count(&activeLinks).Error; err != nil {
		return nil, err
	}
	var totalClicks int64
	if err := p.db.WithContext(ctx).Model(&Link{}).Where("owner_id = ?", orgID).Select("COALESCE(SUM(clicks), 0)").Scan(&totalClicks).Error; err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	sevenDaysAgo := now.AddDate(0, 0, -7)
	thirtyDaysAgo := now.AddDate(0, 0, -30)
	var clicks7d int64
	if err := p.db.WithContext(ctx).Model(&Link{}).Where("owner_id = ? AND created_at >= ?", orgID, sevenDaysAgo).Select("COALESCE(SUM(clicks), 0)").Scan(&clicks7d).Error; err != nil {
		return nil, err
	}
	var clicks30d int64
	if err := p.db.WithContext(ctx).Model(&Link{}).Where("owner_id = ? AND created_at >= ?", orgID, thirtyDaysAgo).Select("COALESCE(SUM(clicks), 0)").Scan(&clicks30d).Error; err != nil {
		return nil, err
	}
	topLinks := make([]LinkMetricOut, 0)
	if err := p.db.WithContext(ctx).Model(&Link{}).Select("id, host, slug, title, clicks").Where("owner_id = ? AND archived = ?", orgID, false).Order("clicks DESC, id ASC").Limit(10).Scan(&topLinks).Error; err != nil {
		return nil, err
	}
	res := TrafficMetricsResource{
		OrgID:       orgID,
		TotalLinks:  totalLinks,
		ActiveLinks: activeLinks,
		TotalClicks: totalClicks,
		Clicks7d:    clicks7d,
		Clicks30d:   clicks30d,
		TopLinks:    topLinks,
		GeneratedAt: now,
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
