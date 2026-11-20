package links

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/octarq-org/octarq/server/plugin"
)

func TestAdversarial_UpdateShortlink_PayloadAndID(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)
	ctx1 := plugin.WithOrgID(context.Background(), 1)

	_, raw, err := p.mcpCreateLink(ctx1, nil, createLinkInput{
		Target: "https://initial.example.com",
		Slug:   "adv-update-base",
		Title:  "Initial Base",
	})
	if err != nil {
		t.Fatalf("failed to create base link: %v", err)
	}
	baseLink := raw.(linkOut)

	if _, _, err := p.mcpUpdateLink(ctx1, nil, updateLinkInput{ID: 0}); err == nil {
		t.Error("expected error for ID = 0")
	}

	target := "https://valid.example.com"
	if _, _, err := p.mcpUpdateLink(ctx1, nil, updateLinkInput{ID: 99999999, Target: &target}); err == nil {
		t.Error("expected error for non-existent link ID")
	}

	if _, _, err := p.mcpUpdateLink(context.Background(), nil, updateLinkInput{ID: baseLink.ID, Target: &target}); err == nil {
		t.Error("expected errNoOrgInContext when context lacks org ID")
	}

	invalidTargets := []string{
		"javascript:alert(1)",
		"data:text/html,<script>evil()</script>",
		"vbscript:msgbox",
		"file:///etc/passwd",
	}
	for _, badTarget := range invalidTargets {
		bad := badTarget
		_, _, err := p.mcpUpdateLink(ctx1, nil, updateLinkInput{
			ID:     baseLink.ID,
			Target: &bad,
		})
		if err == nil {
			t.Errorf("expected error for dangerous target scheme %q", bad)
		}
	}

	for _, badExp := range invalidTargets {
		bad := badExp
		_, _, err := p.mcpUpdateLink(ctx1, nil, updateLinkInput{
			ID:         baseLink.ID,
			ExpiredURL: &bad,
		})
		if err == nil {
			t.Errorf("expected error for dangerous expiredUrl %q", bad)
		}
	}

	badRules := RoutingRules{
		{Type: "geo", Match: "US", Target: "javascript:evil()"},
	}
	if _, _, err := p.mcpUpdateLink(ctx1, nil, updateLinkInput{
		ID:           baseLink.ID,
		RoutingRules: &badRules,
	}); err == nil {
		t.Error("expected error for dangerous routing rule target")
	}

	badDates := []string{"not-a-date", "2026-13-45", "tomorrow", "2026/10/25"}
	for _, badDate := range badDates {
		bad := badDate
		_, _, err := p.mcpUpdateLink(ctx1, nil, updateLinkInput{
			ID:        baseLink.ID,
			ExpiresAt: &bad,
		})
		if err == nil {
			t.Errorf("expected error for invalid expiresAt %q", bad)
		}
	}

	validDate := "2030-12-31T23:59:59Z"
	_, _, err = p.mcpUpdateLink(ctx1, nil, updateLinkInput{
		ID:        baseLink.ID,
		ExpiresAt: &validDate,
	})
	if err != nil {
		t.Fatalf("failed to update valid expiresAt: %v", err)
	}
	var checkL Link
	p.db.First(&checkL, baseLink.ID)
	if checkL.ExpiresAt == nil || checkL.ExpiresAt.Format(time.RFC3339) != validDate {
		t.Errorf("expiresAt not updated: %+v", checkL.ExpiresAt)
	}

	emptyDate := ""
	_, _, err = p.mcpUpdateLink(ctx1, nil, updateLinkInput{
		ID:        baseLink.ID,
		ExpiresAt: &emptyDate,
	})
	if err != nil {
		t.Fatalf("failed to clear expiresAt: %v", err)
	}
	var cleared Link
	p.db.First(&cleared, baseLink.ID)
	if cleared.ExpiresAt != nil {
		t.Errorf("expected expiresAt to be cleared, got: %v", cleared.ExpiresAt)
	}
}

func TestAdversarial_UpdateShortlink_ConflictingSlugsAndHost(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)
	ctx1 := plugin.WithOrgID(context.Background(), 1)

	_, rawA, err := p.mcpCreateLink(ctx1, nil, createLinkInput{
		Target: "https://alpha.example.com",
		Slug:   "slug-alpha",
	})
	if err != nil {
		t.Fatalf("create Link A: %v", err)
	}
	linkA := rawA.(linkOut)

	_, rawB, err := p.mcpCreateLink(ctx1, nil, createLinkInput{
		Target: "https://beta.example.com",
		Slug:   "slug-beta",
	})
	if err != nil {
		t.Fatalf("create Link B: %v", err)
	}
	linkB := rawB.(linkOut)

	targetSlug := "slug-alpha"
	_, _, err = p.mcpUpdateLink(ctx1, nil, updateLinkInput{
		ID:   linkB.ID,
		Slug: &targetSlug,
	})
	if err == nil {
		t.Error("expected unique constraint error when updating to conflicting slug")
	}

	var checkA, checkB Link
	p.db.First(&checkA, linkA.ID)
	p.db.First(&checkB, linkB.ID)
	if checkA.Slug != "slug-alpha" || checkB.Slug != "slug-beta" {
		t.Fatalf("slugs corrupted after conflict: A=%q, B=%q", checkA.Slug, checkB.Slug)
	}

	sameSlug := "slug-alpha"
	newTitle := "Same Slug Updated Title"
	_, rawOut, err := p.mcpUpdateLink(ctx1, nil, updateLinkInput{
		ID:    linkA.ID,
		Slug:  &sameSlug,
		Title: &newTitle,
	})
	if err != nil {
		t.Fatalf("expected update with identical slug to succeed: %v", err)
	}
	if rawOut.(linkOut).Title != newTitle {
		t.Errorf("title not updated: %+v", rawOut)
	}

	reservedSlug := "admin"
	_, _, err = p.mcpUpdateLink(ctx1, nil, updateLinkInput{
		ID:   linkA.ID,
		Slug: &reservedSlug,
	})
	if err == nil {
		t.Error("expected error when updating to reserved slug 'admin'")
	}

	unownedHost := "unowned-domain.org"
	_, _, err = p.mcpUpdateLink(ctx1, nil, updateLinkInput{
		ID:   linkA.ID,
		Host: &unownedHost,
	})
	if err == nil {
		t.Error("expected error when updating to unowned host")
	}
}

func TestAdversarial_UpdateShortlink_TenantIsolation(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)
	ctx1 := plugin.WithOrgID(context.Background(), 1)
	ctx2 := plugin.WithOrgID(context.Background(), 2)
	ctx3 := plugin.WithOrgID(context.Background(), 3)

	_, rawA, err := p.mcpCreateLink(ctx1, nil, createLinkInput{
		Target: "https://org1.example.com",
		Slug:   "org1-private-link",
		Title:  "Org 1 Secret",
	})
	if err != nil {
		t.Fatalf("create Link in Org 1: %v", err)
	}
	linkID := rawA.(linkOut).ID

	hijackTarget := "https://hacked.example.com"
	hijackTitle := "Hijacked by Org 2"

	_, _, err = p.mcpUpdateLink(ctx2, nil, updateLinkInput{
		ID:     linkID,
		Target: &hijackTarget,
		Title:  &hijackTitle,
	})
	if err == nil {
		t.Fatal("security violation: Org 2 updated Org 1's link")
	}
	if err.Error() != "link not found" {
		t.Errorf("expected 'link not found', got %q", err.Error())
	}

	_, _, err = p.mcpUpdateLink(ctx3, nil, updateLinkInput{
		ID:     linkID,
		Target: &hijackTarget,
	})
	if err == nil {
		t.Fatal("security violation: Org 3 updated Org 1's link")
	}

	var link Link
	p.db.First(&link, linkID)
	if link.Target != "https://org1.example.com" || link.Title != "Org 1 Secret" {
		t.Fatalf("Org 1 link corrupted: %+v", link)
	}
}

func TestAdversarial_ToggleShortlink_StressAndIsolation(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)
	ctx1 := plugin.WithOrgID(context.Background(), 1)
	ctx2 := plugin.WithOrgID(context.Background(), 2)

	var invalidatedKeys []string
	p.deleteCache = func(_ context.Context, key string) error {
		invalidatedKeys = append(invalidatedKeys, key)
		return nil
	}

	_, raw, err := p.mcpCreateLink(ctx1, nil, createLinkInput{
		Target: "https://toggle.example.com",
		Slug:   "adv-toggle",
	})
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	linkID := raw.(linkOut).ID

	if _, _, err := p.mcpToggleShortlink(ctx1, nil, toggleShortlinkInput{ID: 0}); err == nil {
		t.Error("expected error for toggle ID = 0")
	}

	if _, _, err := p.mcpToggleShortlink(ctx1, nil, toggleShortlinkInput{ID: 99999999}); err == nil {
		t.Error("expected error for non-existent ID")
	}

	if _, _, err := p.mcpToggleShortlink(context.Background(), nil, toggleShortlinkInput{ID: linkID}); err == nil {
		t.Error("expected errNoOrgInContext")
	}

	if _, _, err := p.mcpToggleShortlink(ctx2, nil, toggleShortlinkInput{ID: linkID}); err == nil {
		t.Fatal("security violation: Org 2 toggled Org 1's link")
	}

	expectedState := true
	for i := 0; i < 10; i++ {
		expectedState = !expectedState
		res, rawOut, err := p.mcpToggleShortlink(ctx1, nil, toggleShortlinkInput{ID: linkID})
		if err != nil || res.IsError {
			t.Fatalf("toggle iteration %d failed: %v", i, err)
		}
		out := rawOut.(linkOut)
		if out.Enabled != expectedState {
			t.Errorf("iter %d: expected enabled=%v, got %v", i, expectedState, out.Enabled)
		}

		var l Link
		p.db.First(&l, linkID)
		if l.Enabled != expectedState {
			t.Errorf("iter %d DB: expected enabled=%v, got %v", i, expectedState, l.Enabled)
		}
	}

	falseVal := false
	trueVal := true
	if expectedState {
		p.mcpToggleShortlink(ctx1, nil, toggleShortlinkInput{ID: linkID, Enabled: &falseVal})
	}
	_, rawOut, err := p.mcpToggleShortlink(ctx1, nil, toggleShortlinkInput{ID: linkID, Enabled: &falseVal})
	if err != nil {
		t.Fatalf("explicit false failed: %v", err)
	}
	if rawOut.(linkOut).Enabled != false {
		t.Errorf("expected false, got true")
	}

	p.mcpToggleShortlink(ctx1, nil, toggleShortlinkInput{ID: linkID, Enabled: &trueVal})
	_, rawOut, err = p.mcpToggleShortlink(ctx1, nil, toggleShortlinkInput{ID: linkID, Enabled: &trueVal})
	if err != nil {
		t.Fatalf("explicit true failed: %v", err)
	}
	if rawOut.(linkOut).Enabled != true {
		t.Errorf("expected true, got false")
	}

	expectedKey := "link:redirect::adv-toggle"
	foundKey := false
	for _, k := range invalidatedKeys {
		if k == expectedKey {
			foundKey = true
			break
		}
	}
	if !foundKey {
		t.Errorf("expected cache key %q in invalidated keys: %v", expectedKey, invalidatedKeys)
	}
}

func TestAdversarial_GetLinkAnalytics_BoundariesAndEdgeCases(t *testing.T) {
	p, _ := setupFullLinksTestDB(t)
	ctx1 := plugin.WithOrgID(context.Background(), 1)
	ctx2 := plugin.WithOrgID(context.Background(), 2)

	_, raw, err := p.mcpCreateLink(ctx1, nil, createLinkInput{
		Target: "https://analytics.example.com",
		Slug:   "adv-analytics",
	})
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	linkID := raw.(linkOut).ID

	if _, _, err := p.mcpGetLinkAnalytics(ctx1, nil, getLinkAnalyticsInput{ID: 0}); err == nil {
		t.Error("expected error for ID = 0")
	}
	if _, _, err := p.mcpGetLinkAnalytics(ctx1, nil, getLinkAnalyticsInput{ID: 99999999}); err == nil {
		t.Error("expected error for non-existent ID")
	}
	if _, _, err := p.mcpGetLinkAnalytics(context.Background(), nil, getLinkAnalyticsInput{ID: linkID}); err == nil {
		t.Error("expected errNoOrgInContext")
	}

	if _, _, err := p.mcpGetLinkAnalytics(ctx2, nil, getLinkAnalyticsInput{ID: linkID}); err == nil {
		t.Fatal("security violation: Org 2 accessed Org 1 analytics")
	}

	dayCases := []struct {
		inputDays int
		wantDays  int
	}{
		{inputDays: 0, wantDays: 30},
		{inputDays: -1, wantDays: 30},
		{inputDays: -500, wantDays: 30},
		{inputDays: 1, wantDays: 1},
		{inputDays: 7, wantDays: 7},
		{inputDays: 30, wantDays: 30},
		{inputDays: 365, wantDays: 365},
		{inputDays: 366, wantDays: 30},
		{inputDays: 1000000, wantDays: 30},
	}

	for _, tc := range dayCases {
		res, rawOut, err := p.mcpGetLinkAnalytics(ctx1, nil, getLinkAnalyticsInput{
			ID:   linkID,
			Days: tc.inputDays,
		})
		if err != nil || res.IsError {
			t.Fatalf("days=%d failed: %v", tc.inputDays, err)
		}
		stats := rawOut.(map[string]any)
		if stats["days"] != tc.wantDays {
			t.Errorf("days input %d: got %v, want %d", tc.inputDays, stats["days"], tc.wantDays)
		}
		if stats["total"].(int64) != 0 {
			t.Errorf("expected 0 total events, got %v", stats["total"])
		}
	}

	metricCases := []struct {
		inputMetric string
		wantMetric  string
	}{
		{"uv", "uv"},
		{"UV", "uv"},
		{"pv", "pv"},
		{"PV", "pv"},
		{"  Pv  ", "pv"},
		{"invalid", "uv"},
		{"", "uv"},
		{"random_string", "uv"},
	}
	for _, mc := range metricCases {
		_, rawOut, err := p.mcpGetLinkAnalytics(ctx1, nil, getLinkAnalyticsInput{
			ID:     linkID,
			Days:   30,
			Metric: mc.inputMetric,
		})
		if err != nil {
			t.Fatalf("metric %q failed: %v", mc.inputMetric, err)
		}
		stats := rawOut.(map[string]any)
		if stats["metric"] != mc.wantMetric {
			t.Errorf("metric input %q: got %q, want %q", mc.inputMetric, stats["metric"], mc.wantMetric)
		}
	}

	now := time.Now()
	p.db.Create(&LinkEvent{LinkID: linkID, CreatedAt: now, Fingerprint: "user-1", Referer: "https://google.com/search", Device: "Desktop", Country: "US"})
	p.db.Create(&LinkEvent{LinkID: linkID, CreatedAt: now, Fingerprint: "user-1", Referer: "https://google.com/search", Device: "Desktop", Country: "US"})
	p.db.Create(&LinkEvent{LinkID: linkID, CreatedAt: now, Fingerprint: "user-1", Referer: "https://google.com/search", Device: "Desktop", Country: "US"})
	p.db.Create(&LinkEvent{LinkID: linkID, CreatedAt: now, Fingerprint: "user-2", Referer: "https://t.co/abc", Device: "Mobile", Country: "JP"})

	_, rawUV, err := p.mcpGetLinkAnalytics(ctx1, nil, getLinkAnalyticsInput{ID: linkID, Metric: "uv"})
	if err != nil {
		t.Fatalf("query UV: %v", err)
	}
	statsUV := rawUV.(map[string]any)
	if statsUV["total"].(int64) != 4 {
		t.Errorf("UV total events: got %v, want 4", statsUV["total"])
	}
	countriesUV := statsUV["countries"].([]StatKV)
	for _, c := range countriesUV {
		if c.Key == "US" && c.Count != 1 {
			t.Errorf("UV country US: count = %d, want 1", c.Count)
		}
	}

	_, rawPV, err := p.mcpGetLinkAnalytics(ctx1, nil, getLinkAnalyticsInput{ID: linkID, Metric: "pv"})
	if err != nil {
		t.Fatalf("query PV: %v", err)
	}
	statsPV := rawPV.(map[string]any)
	countriesPV := statsPV["countries"].([]StatKV)
	for _, c := range countriesPV {
		if c.Key == "US" && c.Count != 3 {
			t.Errorf("PV country US: count = %d, want 3", c.Count)
		}
	}
}

func TestAdversarial_CapabilitiesEndpoint(t *testing.T) {
	p := New()

	out, err := p.linksCapabilities(context.Background(), nil)
	if err != nil {
		t.Fatalf("capabilities unwired failed: %v", err)
	}
	if out.Body.GeoReady != false || out.Body.Mode != "unavailable" {
		t.Errorf("expected GeoReady=false, mode='unavailable'; got %+v", out.Body)
	}

	p.ctx = &plugin.Context{
		GeoLookup: func(ip string) (string, string, string) {
			return "", "", ""
		},
	}
	out, err = p.linksCapabilities(context.Background(), nil)
	if err != nil {
		t.Fatalf("capabilities empty geo failed: %v", err)
	}
	if out.Body.GeoReady != false || out.Body.Mode != "unavailable" {
		t.Errorf("expected GeoReady=false when GeoLookup returns empty; got %+v", out.Body)
	}

	p.ctx = &plugin.Context{
		GeoLookup: func(ip string) (string, string, string) {
			if ip == "8.8.8.8" {
				return "US", "CA", "Mountain View"
			}
			return "", "", ""
		},
	}
	out, err = p.linksCapabilities(context.Background(), nil)
	if err != nil {
		t.Fatalf("capabilities configured geo failed: %v", err)
	}
	if out.Body.GeoReady != true || out.Body.Mode != "configured" {
		t.Errorf("expected GeoReady=true, mode='configured'; got %+v", out.Body)
	}

	router := http.NewServeMux()
	api := humago.New(router, huma.DefaultConfig("Links Test API", "1.0.0"))
	p.ctx.Huma = api
	p.registerRoutes(p.ctx)

	for _, path := range []string{"/api/links/capabilities", "/api/links/geo-status"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("path %s returned HTTP %d, want 200", path, rec.Code)
		}
		var resp struct {
			GeoReady bool   `json:"geoReady"`
			Mode     string `json:"mode"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response for %s: %v", path, err)
		}
		if !resp.GeoReady || resp.Mode != "configured" {
			t.Errorf("path %s unexpected body: %+v", path, resp)
		}
	}
}

func TestAdversarial_MCPClientSession_EndToEnd(t *testing.T) {
	ctx := context.Background()
	p, _ := setupFullLinksTestDB(t)

	l := Link{
		OrgID:   1,
		Slug:    "mcp-e2e-link",
		Target:  "https://e2e.example.com",
		Enabled: true,
	}
	if err := p.db.Create(&l).Error; err != nil {
		t.Fatalf("seed link: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test-octarq", Version: "1.0"}, nil)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0"}, nil)
	p.RegisterMCP(server)

	sTrans, cTrans := mcp.NewInMemoryTransports()
	sSes, err := server.Connect(plugin.WithOrgID(ctx, 1), sTrans, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer sSes.Close()
	cSes, err := client.Connect(ctx, cTrans, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cSes.Close()

	newTarget := "https://e2e-updated.example.com"
	newTitle := "MCP Updated Title"
	updateRes, err := cSes.CallTool(ctx, &mcp.CallToolParams{
		Name: "octarq_network__update_shortlink",
		Arguments: map[string]any{
			"id":     l.ID,
			"target": newTarget,
			"title":  newTitle,
		},
	})
	if err != nil {
		t.Fatalf("CallTool update_shortlink failed: %v", err)
	}
	if len(updateRes.Content) == 0 {
		t.Fatal("empty response content from update_shortlink")
	}
	text := updateRes.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, newTarget) || !strings.Contains(text, newTitle) {
		t.Errorf("update_shortlink response missing updated fields: %s", text)
	}

	toggleRes, err := cSes.CallTool(ctx, &mcp.CallToolParams{
		Name: "octarq_network__toggle_shortlink",
		Arguments: map[string]any{
			"id": l.ID,
		},
	})
	if err != nil {
		t.Fatalf("CallTool toggle_shortlink failed: %v", err)
	}
	toggleText := toggleRes.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(toggleText, `"enabled": false`) {
		t.Errorf("toggle_shortlink response missing 'enabled: false': %s", toggleText)
	}

	analyticsRes, err := cSes.CallTool(ctx, &mcp.CallToolParams{
		Name: "octarq_network__get_link_analytics",
		Arguments: map[string]any{
			"id":     l.ID,
			"days":   14,
			"metric": "pv",
		},
	})
	if err != nil {
		t.Fatalf("CallTool get_link_analytics failed: %v", err)
	}
	analyticsText := analyticsRes.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(analyticsText, `"days": 14`) || !strings.Contains(analyticsText, `"metric": "pv"`) {
		t.Errorf("get_link_analytics response missing parameters: %s", analyticsText)
	}
}
