package dns

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/octarq-org/octarq-plugins/dns/provider"
	"github.com/octarq-org/octarq/server/plugin"
)

func TestRegisterMCPAddsListDomains(t *testing.T) {
	p, _ := setupFreshTestDB(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	p.RegisterMCP(srv)
	p.RegisterMCP(srv)

	ctx := plugin.WithOrgID(context.Background(), 7)
	acc := ProviderAccount{OrgID: 7, Name: "mcp-acc", Type: "cloudflare"}
	if err := p.db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.db.Create(&Domain{OrgID: 7, Name: "mcp.example.com", ProviderAccountID: acc.ID, ForLink: true}).Error; err != nil {
		t.Fatal(err)
	}
	res, doms, err := p.mcpListDomains(ctx, nil, listDomainsInput{})
	if err != nil {
		t.Fatalf("mcpListDomains: %v", err)
	}
	list := doms.([]domainOut)
	if len(list) != 1 || list[0].Name != "mcp.example.com" || !list[0].ForLink {
		t.Fatalf("mcpListDomains = %+v", list)
	}
	var body []map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &body); err != nil {
		t.Fatalf("tool result is not a JSON array: %v", err)
	}
	if len(body) != 1 || body[0]["name"] != "mcp.example.com" {
		t.Fatalf("tool result payload = %+v", body)
	}

	exported, err := p.mcpExportDomains(ctx, 7)
	if err != nil || len(exported.([]Domain)) != 1 {
		t.Fatalf("mcpExportDomains: %v %+v", err, exported)
	}
}

func TestMCPExportScopedToOrg(t *testing.T) {
	p, _ := setupFreshTestDB(t)
	p.db.Create(&Domain{OrgID: 1, Name: "mine.com"})
	p.db.Create(&Domain{OrgID: 2, Name: "v.com"})
	exported, err := p.mcpExportDomains(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	list := exported.([]Domain)
	if len(list) != 1 || list[0].Name != "v.com" {
		t.Errorf("export must be scoped to the org: %+v", list)
	}
}

func TestJSONResultMarshalError(t *testing.T) {
	_, _, err := jsonResult(make(chan int))
	if err == nil {
		t.Fatal("jsonResult of an unsupported value must error")
		return
	}
}

func TestMCPUpdateDomainConfig(t *testing.T) {
	p, _ := setupFreshTestDB(t)
	p.db.Create(&Domain{OrgID: 1, Name: "cfg.com", ForMail: false, ForLink: false, Note: "init"})

	ctx := plugin.WithOrgID(context.Background(), 1)
	trueVal := true
	res, anyOut, err := p.mcpUpdateDomainConfig(ctx, nil, updateDomainConfigInput{
		ID:      1,
		ForMail: &trueVal,
		ForLink: &trueVal,
		Note:    "updated note",
	})
	if err != nil {
		t.Fatalf("mcpUpdateDomainConfig: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %v", res)
	}
	out := anyOut.(domainOut)
	if !out.ForMail || !out.ForLink {
		t.Errorf("expected forMail and forLink to be true, got %+v", out)
	}

	ctx2 := plugin.WithOrgID(context.Background(), 2)
	_, _, err = p.mcpUpdateDomainConfig(ctx2, nil, updateDomainConfigInput{ID: 1, Note: "hacked"})
	if err == nil {
		t.Error("expected error updating another tenant's domain")
	}
}

type mockDNSProvider struct {
	records []provider.Record
}

func (m *mockDNSProvider) ListRecords(ctx context.Context, zoneID string) ([]provider.Record, error) {
	return m.records, nil
}

func (m *mockDNSProvider) CreateRecord(ctx context.Context, zoneID string, r provider.Record) (provider.Record, error) {
	r.ID = "rec-created-1"
	m.records = append(m.records, r)
	return r, nil
}

func (m *mockDNSProvider) UpdateRecord(ctx context.Context, zoneID string, r provider.Record) (provider.Record, error) {
	for i, existing := range m.records {
		if existing.ID == r.ID {
			m.records[i] = r
			return r, nil
		}
	}
	m.records = append(m.records, r)
	return r, nil
}

func (m *mockDNSProvider) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	for i, existing := range m.records {
		if existing.ID == recordID {
			m.records = append(m.records[:i], m.records[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *mockDNSProvider) ListZones(ctx context.Context) ([]provider.Zone, error) {
	return nil, nil
}

func (m *mockDNSProvider) VerifyZone(ctx context.Context, zoneID string) (string, error) {
	return "api.example.com", nil
}

func TestMCPDNSRecordsLifecycle(t *testing.T) {
	p, _ := setupFreshTestDB(t)

	mockProv := &mockDNSProvider{
		records: []provider.Record{
			{ID: "rec-1", Type: "A", Name: "api", Content: "1.2.3.4", TTL: 300},
		},
	}
	provider.Register("mock-test", func([]byte) (provider.Provider, error) {
		return mockProv, nil
	})

	acc, err := seedVaultProviderAccount(p, 1, "acc1", "mock-test", map[string]any{"apiToken": "token123"})
	if err != nil {
		t.Fatal(err)
	}
	dom := Domain{OrgID: 1, Name: "api.example.com", ProviderAccountID: acc.ID, ZoneID: "zone-123"}
	p.db.Create(&dom)

	ctx := plugin.WithOrgID(context.Background(), 1)

	_, listOut, err := p.mcpListDNSRecords(ctx, nil, listDNSRecordsInput{DomainID: dom.ID})
	if err != nil {
		t.Fatalf("mcpListDNSRecords: %v", err)
	}
	recs := listOut.([]provider.Record)
	if len(recs) != 1 || recs[0].ID != "rec-1" {
		t.Fatalf("expected rec-1, got %+v", recs)
	}

	_, createOut, err := p.mcpSetDNSRecord(ctx, nil, setDNSRecordInput{
		DomainID: dom.ID,
		Type:     "A",
		Name:     "sub",
		Content:  "5.6.7.8",
		TTL:      300,
	})
	if err != nil {
		t.Fatalf("mcpSetDNSRecord create: %v", err)
	}
	created := createOut.(provider.Record)
	if created.ID != "rec-created-1" {
		t.Fatalf("expected rec-created-1, got %+v", created)
	}

	_, updateOut, err := p.mcpSetDNSRecord(ctx, nil, setDNSRecordInput{
		DomainID: dom.ID,
		RecordID: created.ID,
		Type:     "A",
		Name:     "sub",
		Content:  "9.9.9.9",
		TTL:      600,
	})
	if err != nil {
		t.Fatalf("mcpSetDNSRecord update: %v", err)
	}
	updated := updateOut.(provider.Record)
	if updated.Content != "9.9.9.9" {
		t.Fatalf("expected updated content 9.9.9.9, got %+v", updated)
	}

	_, _, err = p.mcpDeleteDNSRecord(ctx, nil, deleteDNSRecordInput{
		DomainID: dom.ID,
		RecordID: created.ID,
	})
	if err != nil {
		t.Fatalf("mcpDeleteDNSRecord: %v", err)
	}
	if len(mockProv.records) != 1 {
		t.Fatalf("expected 1 record left, got %d", len(mockProv.records))
	}
}

func TestMCPDNSRecordsValidation(t *testing.T) {
	p, _ := setupFreshTestDB(t)
	ctx := plugin.WithOrgID(context.Background(), 1)

	_, _, err := p.mcpListDNSRecords(context.Background(), nil, listDNSRecordsInput{DomainID: 1})
	if err == nil {
		t.Error("expected error for no org")
	}

	_, _, err = p.mcpListDNSRecords(ctx, nil, listDNSRecordsInput{DomainID: 99999})
	if err == nil {
		t.Error("expected error for domain not found")
	}

	_, _, err = p.mcpSetDNSRecord(context.Background(), nil, setDNSRecordInput{DomainID: 1})
	if err == nil {
		t.Error("expected error for no org")
	}

	_, _, err = p.mcpSetDNSRecord(ctx, nil, setDNSRecordInput{DomainID: 99999, Type: "A", Name: "sub", Content: "1.1.1.1"})
	if err == nil {
		t.Error("expected error for domain not found")
	}

	_, _, err = p.mcpDeleteDNSRecord(context.Background(), nil, deleteDNSRecordInput{DomainID: 1, RecordID: "r1"})
	if err == nil {
		t.Error("expected error for no org")
	}

	_, _, err = p.mcpDeleteDNSRecord(ctx, nil, deleteDNSRecordInput{DomainID: 99999, RecordID: "r1"})
	if err == nil {
		t.Error("expected error for domain not found")
	}

	_, _, err = p.mcpUpdateDomainConfig(context.Background(), nil, updateDomainConfigInput{ID: 1})
	if err == nil {
		t.Error("expected error for no org")
	}

	_, _, err = p.mcpUpdateDomainConfig(ctx, nil, updateDomainConfigInput{ID: 99999})
	if err == nil {
		t.Error("expected error for domain not found")
	}
}

func connectDNSClientSession(t *testing.T, ctx context.Context, srv *mcp.Server) *mcp.ClientSession {
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

func TestMCPResources_DomainsHealth(t *testing.T) {
	p, _ := setupFreshTestDB(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	p.RegisterMCP(srv)

	now := time.Now().UTC()
	d1 := Domain{OrgID: 10, Name: "active.example.com", ProviderAccountID: 1, ZoneID: "z1", ForMail: true, ForLink: true, CreatedAt: now, UpdatedAt: now}
	d2 := Domain{OrgID: 10, Name: "mailonly.example.com", ProviderAccountID: 1, ZoneID: "z2", ForMail: true, ForLink: false, CreatedAt: now, UpdatedAt: now}
	d3 := Domain{OrgID: 10, Name: "dormant.example.com", ProviderAccountID: 1, ZoneID: "z3", ForMail: false, ForLink: false, CreatedAt: now, UpdatedAt: now}
	dOther := Domain{OrgID: 20, Name: "other.example.com", ProviderAccountID: 2, ZoneID: "z4", ForMail: true, ForLink: true, CreatedAt: now, UpdatedAt: now}

	p.db.Create(&d1)
	p.db.Create(&d2)
	p.db.Create(&d3)
	p.db.Create(&dOther)

	ctx10 := plugin.WithOrgID(context.Background(), 10)
	cs10 := connectDNSClientSession(t, ctx10, srv)

	readRes, err := cs10.ReadResource(ctx10, &mcp.ReadResourceParams{URI: "octarq://domains/health"})
	if err != nil {
		t.Fatalf("ReadResource(domains/health): %v", err)
	}
	var res DomainsHealthResource
	if err := json.Unmarshal([]byte(readRes.Contents[0].Text), &res); err != nil {
		t.Fatalf("unmarshal domains health: %v", err)
	}
	if res.OrgID != 10 {
		t.Errorf("OrgID = %d, want 10", res.OrgID)
	}
	if res.TotalDomains != 3 {
		t.Errorf("TotalDomains = %d, want 3", res.TotalDomains)
	}
	if res.ActiveMailDomains != 2 {
		t.Errorf("ActiveMailDomains = %d, want 2", res.ActiveMailDomains)
	}
	if res.ActiveLinkDomains != 1 {
		t.Errorf("ActiveLinkDomains = %d, want 1", res.ActiveLinkDomains)
	}
	if len(res.Domains) != 3 {
		t.Fatalf("len(Domains) = %d, want 3", len(res.Domains))
	}
	stMap := map[string]string{}
	for _, d := range res.Domains {
		stMap[d.Name] = d.Status
	}
	if stMap["active.example.com"] != "active" {
		t.Errorf("active.example.com status = %s, want active", stMap["active.example.com"])
	}
	if stMap["mailonly.example.com"] != "active" {
		t.Errorf("mailonly.example.com status = %s, want active", stMap["mailonly.example.com"])
	}
	if stMap["dormant.example.com"] != "inactive" {
		t.Errorf("dormant.example.com status = %s, want inactive", stMap["dormant.example.com"])
	}

	ctx20 := plugin.WithOrgID(context.Background(), 20)
	cs20 := connectDNSClientSession(t, ctx20, srv)
	read20, err := cs20.ReadResource(ctx20, &mcp.ReadResourceParams{URI: "octarq://domains/health"})
	if err != nil {
		t.Fatalf("ReadResource(domains/health org20): %v", err)
	}
	var res20 DomainsHealthResource
	if err := json.Unmarshal([]byte(read20.Contents[0].Text), &res20); err != nil {
		t.Fatalf("unmarshal domains health org20: %v", err)
	}
	if res20.OrgID != 20 || res20.TotalDomains != 1 || res20.Domains[0].Name != "other.example.com" {
		t.Errorf("unexpected res20: %+v", res20)
	}

	ctxEmpty := plugin.WithOrgID(context.Background(), 999)
	csEmpty := connectDNSClientSession(t, ctxEmpty, srv)
	readEmpty, err := csEmpty.ReadResource(ctxEmpty, &mcp.ReadResourceParams{URI: "octarq://domains/health"})
	if err != nil {
		t.Fatalf("ReadResource empty: %v", err)
	}
	if !strings.Contains(readEmpty.Contents[0].Text, `"domains": []`) {
		t.Errorf("expected empty array [] for domains, got: %s", readEmpty.Contents[0].Text)
	}

	csNoOrg := connectDNSClientSession(t, context.Background(), srv)
	_, errNoOrg := csNoOrg.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "octarq://domains/health"})
	if errNoOrg == nil {
		t.Error("expected error for zero org, got nil")
	}

	_, errNotFound := cs10.ReadResource(ctx10, &mcp.ReadResourceParams{URI: "octarq://unknown"})
	if errNotFound == nil {
		t.Error("expected error for unknown URI, got nil")
	}
}

func TestMCPResources_ConflictAndCoreConflictPanic(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	addMCPResource(srv, "dns", &mcp.Resource{
		URI: "octarq://domains/conflict-test",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return nil, nil
	})

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic on conflict with different owner")
			}
		}()
		addMCPResource(srv, "other_owner", &mcp.Resource{
			URI: "octarq://domains/conflict-test",
		}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return nil, nil
		})
	}()

	srvCore := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	srvCore.AddResource(&mcp.Resource{
		URI: "octarq://domains/core-existing",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return nil, nil
	})

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic on core conflict")
			}
		}()
		addMCPResource(srvCore, "dns", &mcp.Resource{
			URI: "octarq://domains/core-existing",
		}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return nil, nil
		})
	}()
}
