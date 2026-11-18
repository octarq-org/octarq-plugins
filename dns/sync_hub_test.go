package dns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSyncDomains_ResolvesThroughVaultOnly pins that sync resolves credentials
// solely through VaultService and fails closed when the Vault item is missing
// or invalid.
func TestSyncDomains_ResolvesThroughVaultOnly(t *testing.T) {
	p, mkCtx := setupFreshTestDB(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	ctx := mkCtx(req)

	input := &SyncDomainsInput{Ctx: ctx}
	input.Body.ProviderAccountID = 50

	// Account 50 does not exist in local table or Vault -> 404.
	if _, err := p.syncDomains(context.Background(), input); err == nil {
		t.Fatal("expected 404 for unknown account")
	} else if status := statusOf(t, err); status != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown account, got %d (%v)", status, err)
	}

	// Vault item exists directly (no ProviderAccount row) with an invalid provider -> error.
	mv := p.vaultSvc.(*mockVaultService)
	acc, err := seedVaultProviderAccount(p, 1, "Invalid prov", "not-a-provider", map[string]any{"apiToken": "tok"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	input.Body.ProviderAccountID = acc.ID
	if _, err := p.syncDomains(context.Background(), input); err == nil {
		t.Fatal("expected an unusable vault credential to fail the sync")
	}

	// Vault read error -> 500 fail closed.
	mv.getErr = errors.New("vault locked")
	if _, err := p.syncDomains(context.Background(), input); err == nil || statusOf(t, err) != http.StatusInternalServerError {
		t.Fatalf("expected 500 on vault read error, got %v", err)
	}
}
