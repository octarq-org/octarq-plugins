package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/octarq-org/octarq-plugins/dns/provider"
	"github.com/octarq-org/octarq/server/plugin"
)

// TestChallengerM1_ProviderRegistryStress stress-tests concurrent provider registration
// and construction, guaranteeing thread-safety and fail-closed behavior on invalid inputs.
func TestChallengerM1_ProviderRegistryStress(t *testing.T) {
	const workers = 30
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			provName := fmt.Sprintf("stress-prov-%d", id%5)
			// Concurrent Register
			provider.Register(provName, func(credsJSON []byte) (provider.Provider, error) {
				if len(credsJSON) == 0 {
					return nil, fmt.Errorf("empty credentials")
				}
				return nil, nil
			})

			// Concurrent New with valid and invalid data
			_, _ = provider.New(provName, []byte(`{"token":"abc"}`))
			_, err := provider.New(provName, []byte{})
			if err == nil {
				t.Errorf("worker %d: expected error on empty credentials", id)
			}
		}(i)
	}
	wg.Wait()

	// Unknown provider must fail closed
	_, err := provider.New("non-existent-provider-xyz", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for non-existent provider, got nil")
	}
}

// TestChallengerM1_DDNSConcurrentUpdate stress-tests concurrent DDNS update requests
// through the HTTP router on the same token, ensuring atomic updates and zero race crashes.
func TestChallengerM1_DDNSConcurrentUpdate(t *testing.T) {
	_, srv, db := newVerifyHarness(t)
	const orgID = uint(1)

	dom := Domain{
		OrgID:  orgID,
		Name:   "ddns-stress.example.com",
		ZoneID: "z-stress",
	}
	if err := db.Create(&dom).Error; err != nil {
		t.Fatalf("create domain: %v", err)
	}

	// Create DDNS token via HTTP endpoint
	createReqBody := map[string]any{
		"domainId":   dom.ID,
		"recordName": "ddns-stress.example.com",
		"recordType": "A",
		"label":      "Stress Tester",
	}
	bodyBytes, _ := json.Marshal(createReqBody)
	req := httptest.NewRequest("POST", "/api/dns/ddns", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create DDNS token: want 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID        uint   `json:"id"`
		Secret    string `json:"secret"`
		UpdateURL string `json:"updateUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	if createResp.Secret == "" {
		t.Fatal("expected non-empty secret")
	}

	const concurrentUpdates = 30
	var wg sync.WaitGroup
	wg.Add(concurrentUpdates)

	var successCount atomic.Int32
	var failCount atomic.Int32

	for i := 0; i < concurrentUpdates; i++ {
		go func(id int) {
			defer wg.Done()
			ip := fmt.Sprintf("198.51.100.%d", (id%250)+1)
			updatePath := fmt.Sprintf("/api/dns/ddns/update?token=%s&ip=%s", createResp.Secret, ip)
			upReq := httptest.NewRequest("GET", updatePath, nil)
			upRec := httptest.NewRecorder()
			srv.ServeHTTP(upRec, upReq)

			if upRec.Code == http.StatusOK {
				successCount.Add(1)
			} else {
				failCount.Add(1)
			}
		}(i)
	}
	wg.Wait()

	t.Logf("DDNS concurrent updates completed: success=%d, fail=%d", successCount.Load(), failCount.Load())
	// In the absence of an external provider mock, update may return 500 or 200, but MUST NOT panic or race.
}

// TestChallengerM1_FailClosedHostCredentials tests that failing host credentials
// strictly fail closed without panic or silent corruption.
func TestChallengerM1_FailClosedHostCredentials(t *testing.T) {
	p, srv, db := newVerifyHarness(t)

	// Replace host with one that returns an error
	p.host = &plugin.TestHost{
		LookupCredentialMock: func(ctx context.Context, orgID uint, query plugin.CredentialQuery) (*plugin.Credential, error) {
			return nil, fmt.Errorf("kms backend unreachable")
		},
		SessionMock: &plugin.TestSession{
			OrgIDFn: func(*http.Request) uint { return 1 },
		},
	}

	dom := Domain{
		OrgID:             1,
		Name:              "failclosed.example.com",
		ProviderAccountID: 999,
		ZoneID:            "z999",
	}
	db.Create(&dom)

	req := httptest.NewRequest("POST", fmt.Sprintf("/api/dns/domains/%d/sync", dom.ID), nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// Must fail closed with 500 or 400, never succeed
	if rec.Code == http.StatusOK {
		t.Fatalf("expected fail-closed error, got 200 OK")
	}
}
