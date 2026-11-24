package links

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/octarq-org/octarq/server/plugin"
)

func TestMCPLinksLifecycle(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)

	ctx1 := plugin.WithOrgID(context.Background(), 1)
	ctx2 := plugin.WithOrgID(context.Background(), 2)

	enabled := true
	res, rawOut, err := p.mcpCreateLink(ctx1, nil, createLinkInput{
		Target:  "https://example.com/hello",
		Slug:    "my-slug",
		Title:   "My Title",
		Tags:    "tag1,tag2",
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("mcpCreateLink: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error result")
	}
	created := rawOut.(linkOut)
	if created.Slug != "my-slug" || created.Target != "https://example.com/hello" {
		t.Fatalf("unexpected linkOut: %+v", created)
	}

	newTarget := "https://example.com/updated"
	_, _, err = p.mcpUpdateLink(ctx2, nil, updateLinkInput{
		ID:     created.ID,
		Target: &newTarget,
	})
	if err == nil {
		t.Error("expected error when org 2 tries to update org 1 link")
	}

	newTitle := "Updated Title"
	_, rawOut, err = p.mcpUpdateLink(ctx1, nil, updateLinkInput{
		ID:     created.ID,
		Target: &newTarget,
		Title:  &newTitle,
	})
	if err != nil {
		t.Fatalf("mcpUpdateLink: %v", err)
	}
	updated := rawOut.(linkOut)
	if updated.Target != newTarget || updated.Title != newTitle {
		t.Fatalf("unexpected updated link: %+v", updated)
	}

	_, rawBatch, err := p.mcpBatchCreateLinks(ctx1, nil, batchCreateLinksInput{
		Links: []createLinkInput{
			{Target: "https://example.com/b1", Slug: "b1"},
			{Target: "https://example.com/b2", Slug: "b2"},
		},
	})
	if err != nil {
		t.Fatalf("mcpBatchCreateLinks: %v", err)
	}
	batch := rawBatch.([]linkOut)
	if len(batch) != 2 {
		t.Fatalf("expected 2 links created in batch, got %d", len(batch))
	}

	_, rawList, err := p.mcpListLinks(ctx1, nil, listLinksInput{
		Tag: "tag1",
	})
	if err != nil {
		t.Fatalf("mcpListLinks: %v", err)
	}
	linksList := rawList.([]linkOut)
	if len(linksList) != 1 || linksList[0].Slug != "my-slug" {
		t.Fatalf("expected 1 tagged link, got %+v", linksList)
	}

	_, _, err = p.mcpDeleteLink(ctx1, nil, deleteLinkInput{
		ID: created.ID,
	})
	if err != nil {
		t.Fatalf("mcpDeleteLink: %v", err)
	}

	_, _, err = p.mcpUpdateLink(ctx1, nil, updateLinkInput{
		ID:     created.ID,
		Target: &newTarget,
	})
	if err == nil {
		t.Error("expected error updating deleted link")
	}
}

func TestMCPCreateLinkValidation(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)
	ctx := plugin.WithOrgID(context.Background(), 1)

	_, _, err := p.mcpCreateLink(ctx, nil, createLinkInput{Target: ""})
	if err == nil {
		t.Error("expected error for empty target")
	}

	_, _, err = p.mcpCreateLink(ctx, nil, createLinkInput{Target: "javascript:alert(1)"})
	if err == nil {
		t.Error("expected error for invalid target scheme")
	}

	_, _, err = p.mcpCreateLink(context.Background(), nil, createLinkInput{Target: "https://ok.com"})
	if err == nil {
		t.Error("expected error when no org in context")
	}
}

func TestMCPWriteEdgeCases(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)
	ctx := plugin.WithOrgID(context.Background(), 1)

	_, _, err := p.mcpCreateLink(ctx, nil, createLinkInput{
		Target: "https://example.com",
		Slug:   "admin",
	})
	if err == nil {
		t.Error("expected error for reserved slug")
	}

	_, _, err = p.mcpCreateLink(ctx, nil, createLinkInput{
		Target: "https://example.com",
		Host:   "unowned.com",
	})
	if err == nil {
		t.Error("expected error for unowned host")
	}

	invTarget := "ftp://bad"
	_, _, err = p.mcpUpdateLink(ctx, nil, updateLinkInput{
		ID:     999999,
		Target: &invTarget,
	})
	if err == nil {
		t.Error("expected error for link not found")
	}

	_, _, err = p.mcpUpdateLink(ctx, nil, updateLinkInput{
		ID: 0,
	})
	if err == nil {
		t.Error("expected error for ID=0")
	}

	_, _, err = p.mcpUpdateLink(context.Background(), nil, updateLinkInput{
		ID: 1,
	})
	if err == nil {
		t.Error("expected error for no org")
	}

	_, _, err = p.mcpDeleteLink(ctx, nil, deleteLinkInput{ID: 0})
	if err == nil {
		t.Error("expected error for delete ID=0")
	}

	_, _, err = p.mcpDeleteLink(context.Background(), nil, deleteLinkInput{ID: 1})
	if err == nil {
		t.Error("expected error for delete no org")
	}

	_, _, err = p.mcpDeleteLink(ctx, nil, deleteLinkInput{ID: 999999})
	if err == nil {
		t.Error("expected error for delete not found")
	}

	_, _, err = p.mcpBatchCreateLinks(ctx, nil, batchCreateLinksInput{})
	if err == nil {
		t.Error("expected error for empty batch")
	}

	_, _, err = p.mcpBatchCreateLinks(ctx, nil, batchCreateLinksInput{
		Links: []createLinkInput{
			{Target: "javascript:bad"},
		},
	})
	if err == nil {
		t.Error("expected error for invalid link in batch")
	}

	_, _, err = p.mcpListLinks(context.Background(), nil, listLinksInput{})
	if err == nil {
		t.Error("expected error for list no org")
	}

	exp, err := p.mcpExportLinks(ctx, 1)
	if err != nil {
		t.Fatalf("mcpExportLinks: %v", err)
	}
	if exp == nil {
		t.Error("expected non-nil export")
	}
}

func TestMCPRegisteredToolsIncludeAllRequired(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	p := New()
	p.RegisterMCP(server)

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	required := []string{
		"octarq_network__list_links",
		"octarq_network__create_link",
		"octarq_network__update_link",
		"octarq_network__update_shortlink",
		"octarq_network__toggle_shortlink",
		"octarq_network__get_link_analytics",
		"octarq_network__delete_link",
		"octarq_network__batch_create_links",
	}
	for _, reqName := range required {
		if !slicesContains(names, reqName) {
			t.Errorf("tool %q not registered; got: %v", reqName, names)
		}
	}
}

func TestMCPUpdateLinkFullFields(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)
	ctx := plugin.WithOrgID(context.Background(), 1)

	res, rawOut, err := p.mcpCreateLink(ctx, nil, createLinkInput{
		Target: "https://example.com/initial",
		Slug:   "initial-slug",
		Title:  "Initial Title",
	})
	if err != nil || res.IsError {
		t.Fatalf("create failed: %v", err)
	}
	created := rawOut.(linkOut)

	newTarget := "https://example.com/updated-full"
	newSlug := "updated-slug"
	newHost := ""
	newTitle := "Updated Title Full"
	newTags := "tagA,tagB"
	newNote := "Updated administrative note"
	newPass := "secret123"
	newLimit := int64(500)
	expStr := "2030-01-01T00:00:00Z"
	expURL := "https://example.com/expired"
	rules := RoutingRules{
		{Type: "geo", Match: "US", Target: "https://us.example.com"},
		{Type: "split", Weight: 50, Target: "https://b.example.com"},
	}
	enabled := false
	archived := true

	updateRes, rawUpdated, err := p.mcpUpdateLink(ctx, nil, updateLinkInput{
		ID:           created.ID,
		Target:       &newTarget,
		Slug:         &newSlug,
		Host:         &newHost,
		Title:        &newTitle,
		Tags:         &newTags,
		Note:         &newNote,
		Password:     &newPass,
		ClickLimit:   &newLimit,
		ExpiresAt:    &expStr,
		ExpiredURL:   &expURL,
		RoutingRules: &rules,
		Enabled:      &enabled,
		Archived:     &archived,
	})
	if err != nil || updateRes.IsError {
		t.Fatalf("update full fields failed: %v", err)
	}
	out := rawUpdated.(linkOut)
	if out.Slug != newSlug || out.Target != newTarget || out.Title != newTitle || out.Tags != newTags || out.Enabled != enabled || out.Archived != archived {
		t.Fatalf("unexpected linkOut: %+v", out)
	}

	var l Link
	if err := p.db.Where("id = ?", created.ID).First(&l).Error; err != nil {
		t.Fatalf("query DB: %v", err)
	}
	if l.Password != newPass {
		t.Errorf("password = %q, want %q", l.Password, newPass)
	}
	if l.ClickLimit != newLimit {
		t.Errorf("clickLimit = %d, want %d", l.ClickLimit, newLimit)
	}
	if l.ExpiredURL != expURL {
		t.Errorf("expiredUrl = %q, want %q", l.ExpiredURL, expURL)
	}
	if l.ExpiresAt == nil || l.ExpiresAt.Format(time.RFC3339) != expStr {
		t.Errorf("expiresAt = %v, want %s", l.ExpiresAt, expStr)
	}
	if len(l.RoutingRules) != 2 || l.RoutingRules[0].Match != "US" {
		t.Errorf("routingRules mismatch: %+v", l.RoutingRules)
	}
}

func TestMCPToggleShortlink(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)
	ctx := plugin.WithOrgID(context.Background(), 1)

	_, rawOut, err := p.mcpCreateLink(ctx, nil, createLinkInput{
		Target: "https://example.com/toggle",
		Slug:   "toggle-slug",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	linkID := rawOut.(linkOut).ID

	res, rawOut, err := p.mcpToggleShortlink(ctx, nil, toggleShortlinkInput{ID: linkID})
	if err != nil || res.IsError {
		t.Fatalf("toggle without param failed: %v", err)
	}
	if rawOut.(linkOut).Enabled != false {
		t.Errorf("expected Enabled == false, got true")
	}

	res, rawOut, err = p.mcpToggleShortlink(ctx, nil, toggleShortlinkInput{ID: linkID})
	if err != nil || res.IsError {
		t.Fatalf("toggle without param 2 failed: %v", err)
	}
	if rawOut.(linkOut).Enabled != true {
		t.Errorf("expected Enabled == true, got false")
	}

	f := false
	res, rawOut, err = p.mcpToggleShortlink(ctx, nil, toggleShortlinkInput{ID: linkID, Enabled: &f})
	if err != nil || res.IsError {
		t.Fatalf("toggle explicit false failed: %v", err)
	}
	if rawOut.(linkOut).Enabled != false {
		t.Errorf("expected Enabled == false, got true")
	}

	if _, _, err := p.mcpToggleShortlink(ctx, nil, toggleShortlinkInput{ID: 0}); err == nil {
		t.Error("expected error for ID=0")
	}

	ctx2 := plugin.WithOrgID(context.Background(), 2)
	if _, _, err := p.mcpToggleShortlink(ctx2, nil, toggleShortlinkInput{ID: linkID}); err == nil {
		t.Error("expected error for cross-tenant toggle")
	}
}

func TestMCPGetLinkAnalytics(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)
	ctx := plugin.WithOrgID(context.Background(), 1)

	_, rawOut, err := p.mcpCreateLink(ctx, nil, createLinkInput{
		Target: "https://example.com/stats",
		Slug:   "stats-slug",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	linkID := rawOut.(linkOut).ID

	now := time.Now()
	p.db.Create(&LinkEvent{LinkID: linkID, CreatedAt: now, Country: "US", Device: "Mobile", Referer: "https://google.com", Fingerprint: "fp1"})
	p.db.Create(&LinkEvent{LinkID: linkID, CreatedAt: now, Country: "US", Device: "Desktop", Referer: "https://google.com", Fingerprint: "fp1"})
	p.db.Create(&LinkEvent{LinkID: linkID, CreatedAt: now, Country: "JP", Device: "Mobile", Referer: "https://twitter.com", Fingerprint: "fp2"})

	res, rawStats, err := p.mcpGetLinkAnalytics(ctx, nil, getLinkAnalyticsInput{
		ID:     linkID,
		Days:   30,
		Metric: "uv",
	})
	if err != nil || res.IsError {
		t.Fatalf("get analytics failed: %v", err)
	}
	stats := rawStats.(map[string]any)
	if stats["total"].(int64) != 3 {
		t.Errorf("total = %v, want 3", stats["total"])
	}
	if stats["metric"] != "uv" {
		t.Errorf("metric = %v, want uv", stats["metric"])
	}

	_, rawStatsPV, err := p.mcpGetLinkAnalytics(ctx, nil, getLinkAnalyticsInput{
		ID:     linkID,
		Days:   7,
		Metric: "pv",
	})
	if err != nil {
		t.Fatalf("get analytics pv failed: %v", err)
	}
	statsPV := rawStatsPV.(map[string]any)
	if statsPV["metric"] != "pv" {
		t.Errorf("metric = %v, want pv", statsPV["metric"])
	}

	ctx2 := plugin.WithOrgID(context.Background(), 2)
	if _, _, err := p.mcpGetLinkAnalytics(ctx2, nil, getLinkAnalyticsInput{ID: linkID}); err == nil {
		t.Error("expected error for cross-tenant get analytics")
	}
}

func TestLinksCapabilities(t *testing.T) {
	p := New()
	out, err := p.linksCapabilities(context.Background(), nil)
	if err != nil {
		t.Fatalf("linksCapabilities: %v", err)
	}
	if out.Body.GeoReady != false || out.Body.Mode != "unavailable" {
		t.Errorf("unwired capabilities: %+v", out.Body)
	}

	p.ctx = &plugin.Context{
		GeoLookup: func(ip string) (string, string, string) {
			if ip == "8.8.8.8" {
				return "US", "CA", "Mountain View"
			}
			return "", "", ""
		},
	}
	outReady, err := p.linksCapabilities(context.Background(), nil)
	if err != nil {
		t.Fatalf("linksCapabilities ready: %v", err)
	}
	if outReady.Body.GeoReady != true || outReady.Body.Mode != "configured" {
		t.Errorf("wired capabilities: %+v", outReady.Body)
	}
}

func connectTestSession(t *testing.T, ctx context.Context, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })
	return clientSession
}

func TestMCPResources_LinksTrendingAndTraffic(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	p.RegisterMCP(server)
	p.RegisterMCP(server)

	now := time.Now().UTC()
	l1 := Link{OrgID: 1, Host: "go.corp.com", Slug: "l1", Title: "Link 1", Clicks: 50, Enabled: true, Archived: false, CreatedAt: now.AddDate(0, 0, -2)}
	l2 := Link{OrgID: 1, Host: "go.corp.com", Slug: "l2", Title: "Link 2", Clicks: 30, Enabled: true, Archived: false, CreatedAt: now.AddDate(0, 0, -10)}
	l3 := Link{OrgID: 1, Host: "go.corp.com", Slug: "l3", Title: "Link 3", Clicks: 20, Enabled: false, Archived: true, CreatedAt: now.AddDate(0, 0, -40)}
	lOrg2 := Link{OrgID: 2, Host: "other.com", Slug: "secret", Title: "Secret", Clicks: 500, Enabled: true, Archived: false, CreatedAt: now}

	p.db.Create(&l1)
	p.db.Create(&l2)
	p.db.Create(&l3)
	p.db.Create(&lOrg2)

	ctx1 := plugin.WithOrgID(context.Background(), 1)
	cs1 := connectTestSession(t, ctx1, server)

	readTrending, err := cs1.ReadResource(ctx1, &mcp.ReadResourceParams{URI: "octarq://links/trending"})
	if err != nil {
		t.Fatalf("ReadResource(trending): %v", err)
	}
	var trend TrendingLinksResource
	if err := json.Unmarshal([]byte(readTrending.Contents[0].Text), &trend); err != nil {
		t.Fatalf("unmarshal trending JSON: %v", err)
	}
	if trend.OrgID != 1 {
		t.Errorf("expected OrgID = 1, got %d", trend.OrgID)
	}
	if len(trend.Links) != 2 {
		t.Fatalf("expected 2 unarchived links, got %d", len(trend.Links))
	}
	if trend.Links[0].Slug != "l1" || trend.Links[0].Clicks != 50 {
		t.Errorf("expected top link l1 with 50 clicks, got %+v", trend.Links[0])
	}
	if trend.Links[1].Slug != "l2" || trend.Links[1].Clicks != 30 {
		t.Errorf("expected second link l2 with 30 clicks, got %+v", trend.Links[1])
	}

	ctx2 := plugin.WithOrgID(context.Background(), 2)
	cs2 := connectTestSession(t, ctx2, server)

	readTrending2, err := cs2.ReadResource(ctx2, &mcp.ReadResourceParams{URI: "octarq://links/trending"})
	if err != nil {
		t.Fatalf("ReadResource(trending org2): %v", err)
	}
	var trend2 TrendingLinksResource
	if err := json.Unmarshal([]byte(readTrending2.Contents[0].Text), &trend2); err != nil {
		t.Fatalf("unmarshal trending2 JSON: %v", err)
	}
	if trend2.OrgID != 2 || len(trend2.Links) != 1 || trend2.Links[0].Slug != "secret" {
		t.Errorf("unexpected trend2: %+v", trend2)
	}

	readTraffic, err := cs1.ReadResource(ctx1, &mcp.ReadResourceParams{URI: "octarq://metrics/traffic"})
	if err != nil {
		t.Fatalf("ReadResource(traffic): %v", err)
	}
	var traffic TrafficMetricsResource
	if err := json.Unmarshal([]byte(readTraffic.Contents[0].Text), &traffic); err != nil {
		t.Fatalf("unmarshal traffic JSON: %v", err)
	}
	if traffic.OrgID != 1 {
		t.Errorf("traffic OrgID = %d, want 1", traffic.OrgID)
	}
	if traffic.TotalLinks != 3 {
		t.Errorf("traffic TotalLinks = %d, want 3", traffic.TotalLinks)
	}
	if traffic.ActiveLinks != 2 {
		t.Errorf("traffic ActiveLinks = %d, want 2", traffic.ActiveLinks)
	}
	if traffic.TotalClicks != 100 {
		t.Errorf("traffic TotalClicks = %d, want 100", traffic.TotalClicks)
	}
	if traffic.Clicks7d != 50 {
		t.Errorf("traffic Clicks7d = %d, want 50 (l1 created 2 days ago)", traffic.Clicks7d)
	}
	if traffic.Clicks30d != 80 {
		t.Errorf("traffic Clicks30d = %d, want 80 (l1 + l2)", traffic.Clicks30d)
	}
	if len(traffic.TopLinks) != 2 {
		t.Errorf("traffic TopLinks count = %d, want 2", len(traffic.TopLinks))
	}

	ctxEmpty := plugin.WithOrgID(context.Background(), 999)
	csEmpty := connectTestSession(t, ctxEmpty, server)

	readEmpty, err := csEmpty.ReadResource(ctxEmpty, &mcp.ReadResourceParams{URI: "octarq://links/trending"})
	if err != nil {
		t.Fatalf("ReadResource empty org: %v", err)
	}
	if !strings.Contains(readEmpty.Contents[0].Text, `"links": []`) {
		t.Errorf("expected empty array [] for links, got: %s", readEmpty.Contents[0].Text)
	}

	readTrafficEmpty, err := csEmpty.ReadResource(ctxEmpty, &mcp.ReadResourceParams{URI: "octarq://metrics/traffic"})
	if err != nil {
		t.Fatalf("ReadResource traffic empty org: %v", err)
	}
	if !strings.Contains(readTrafficEmpty.Contents[0].Text, `"top_links": []`) {
		t.Errorf("expected empty array [] for top_links, got: %s", readTrafficEmpty.Contents[0].Text)
	}

	csNoOrg := connectTestSession(t, context.Background(), server)
	_, errNoOrg := csNoOrg.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "octarq://links/trending"})
	if errNoOrg == nil {
		t.Error("expected error for unauthenticated/zero-org context, got nil")
	}

	_, errNotFound := cs1.ReadResource(ctx1, &mcp.ReadResourceParams{URI: "octarq://unknown/resource"})
	if errNotFound == nil {
		t.Error("expected error for unknown resource URI")
	}
}

func TestMCPResources_ConflictPanic(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	addMCPResource(srv, "links", &mcp.Resource{
		URI: "octarq://links/test-conflict",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return nil, nil
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on conflict, got none")
		}
	}()

	addMCPResource(srv, "other_plugin", &mcp.Resource{
		URI: "octarq://links/test-conflict",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return nil, nil
	})
}

func TestMCPResources_CoreConflictPanic(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	srv.AddResource(&mcp.Resource{
		URI: "octarq://metrics/traffic-core-existing",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return nil, nil
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on core conflict, got none")
		}
	}()

	addMCPResource(srv, "links", &mcp.Resource{
		URI: "octarq://metrics/traffic-core-existing",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return nil, nil
	})
}
