package dns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/octarq-org/octarq/server/plugin"
)

// TestChallenger_DNSCredentialBridgeStress stress tests DNS credential resolution,
// cross-tenant isolation, and invalid configurations via VaultService.
func TestChallenger_DNSCredentialBridgeStress(t *testing.T) {
	p, _ := setupFreshTestDB(t)
	mv := p.vaultSvc.(*mockVaultService)
	ctx := context.Background()

	mustPut := func(orgID uint, provider string, payload map[string]any) uint {
		meta, err := mv.PutSecret(ctx, plugin.PutSecretRequest{
			OrgID:        orgID,
			Scope:        plugin.VaultScopeOrg,
			ItemType:     "integration",
			ProviderType: "dns",
			Provider:     provider,
			Name:         provider,
			Payload:      payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		return meta.ID
	}

	idOrg10Valid := mustPut(10, "cloudflare", map[string]any{"apiToken": "org10-cf-token"})
	idOrg10EmptyToken := mustPut(10, "cloudflare", map[string]any{"apiToken": ""})
	idOrg10EmptyPayload := mustPut(10, "cloudflare", nil)
	idOrg20Valid := mustPut(20, "cloudflare", map[string]any{"apiToken": "org20-cf-token"})

	accOrg10, err := seedVaultProviderAccount(p, 10, "Org 10 Account", "cloudflare", map[string]any{"apiToken": "oss-token-org10"})
	if err != nil {
		t.Fatal(err)
	}
	accOrg20, err := seedVaultProviderAccount(p, 20, "Org 20 Account", "cloudflare", map[string]any{"apiToken": "oss-token-org20"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Cross-Tenant Isolation Tests
	t.Run("CrossTenant_ownsProviderAccount", func(t *testing.T) {
		p.orgID = func(r *http.Request) uint { return 20 }
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		if owned, err := p.ownsProviderAccount(req, idOrg10Valid); owned || err != nil {
			t.Errorf("CRITICAL SECURITY FAILURE: ownsProviderAccount(%d) returned %v, %v for Org 20", idOrg10Valid, owned, err)
		}
		if owned, err := p.ownsProviderAccount(req, idOrg20Valid); !owned || err != nil {
			t.Errorf("ownsProviderAccount(%d) = %v, %v for Org 20", idOrg20Valid, owned, err)
		}
		if owned, err := p.ownsProviderAccount(req, accOrg20.ID); !owned || err != nil {
			t.Errorf("ownsProviderAccount(%d) = %v, %v for Org 20", accOrg20.ID, owned, err)
		}
		if owned, err := p.ownsProviderAccount(req, accOrg10.ID); owned || err != nil {
			t.Errorf("ownsProviderAccount(%d) = %v, %v for Org 20 (cross-org)", accOrg10.ID, owned, err)
		}
	})

	t.Run("CrossTenant_providerFor", func(t *testing.T) {
		dom := Domain{
			OrgID:             20,
			Name:              "cross-test.com",
			ProviderAccountID: idOrg10Valid,
		}
		prov, err := p.providerFor(dom)
		if err == nil {
			t.Errorf("CRITICAL SECURITY FAILURE: providerFor resolved Org 10's credential for Org 20! prov=%+v", prov)
		}
		if !strings.Contains(err.Error(), "provider account not found") {
			t.Errorf("expected 'provider account not found', got: %v", err)
		}
	})

	// 2. Resolution & Fail-Closed Tests
	t.Run("VaultResolution", func(t *testing.T) {
		prov, err := p.providerFor(Domain{OrgID: 10, ProviderAccountID: idOrg10Valid})
		if err != nil || prov == nil {
			t.Fatalf("expected Vault resolution to succeed, err=%v", err)
		}

		provAcc, err := p.providerFor(Domain{OrgID: 10, ProviderAccountID: accOrg10.ID})
		if err != nil || provAcc == nil {
			t.Fatalf("expected ProviderAccount+Vault resolution to succeed, err=%v", err)
		}

		_, err = p.providerFor(Domain{OrgID: 10, ProviderAccountID: 9999})
		if err == nil || !strings.Contains(err.Error(), "provider account not found") {
			t.Errorf("expected 'provider account not found', got: %v", err)
		}

		_, err = p.providerFor(Domain{OrgID: 10, ProviderAccountID: 0})
		if err == nil || !strings.Contains(err.Error(), "has no provider account configured") {
			t.Errorf("expected 'has no provider account configured', got: %v", err)
		}
	})

	// 3. Invalid Configurations Tests
	t.Run("InvalidConfigurations_MissingAPIToken", func(t *testing.T) {
		_, err := p.providerFor(Domain{OrgID: 10, ProviderAccountID: idOrg10EmptyToken})
		if err == nil || !strings.Contains(err.Error(), "apiToken required") {
			t.Errorf("expected 'apiToken required' error, got: %v", err)
		}
	})

	t.Run("InvalidConfigurations_EmptyPayload", func(t *testing.T) {
		_, err := p.providerFor(Domain{OrgID: 10, ProviderAccountID: idOrg10EmptyPayload})
		if !errors.Is(err, errProviderCredentials) {
			t.Fatalf("expected errProviderCredentials for an empty integration, got %v", err)
		}
	})
}
