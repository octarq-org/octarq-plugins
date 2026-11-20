package dns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSyncDomains_ResolvesThroughVaultOnly(t *testing.T) {
	p, mkCtx := setupFreshTestDB(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	ctx := mkCtx(req)

	input := &SyncDomainsInput{Ctx: ctx}
	input.Body.ProviderAccountID = 50

	if _, err := p.syncDomains(context.Background(), input); err == nil {
		t.Fatal("expected 404 for unknown account")
	} else if status := statusOf(t, err); status != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown account, got %d (%v)", status, err)
	}

	mv := p.vaultSvc.(*mockVaultService)
	acc, err := seedVaultProviderAccount(p, 1, "Invalid prov", "not-a-provider", map[string]any{"apiToken": "tok"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	input.Body.ProviderAccountID = acc.ID
	if _, err := p.syncDomains(context.Background(), input); err == nil {
		t.Fatal("expected an unusable vault credential to fail the sync")
	}

	mv.getErr = errors.New("vault locked")
	if _, err := p.syncDomains(context.Background(), input); err == nil || statusOf(t, err) != http.StatusInternalServerError {
		t.Fatalf("expected 500 on vault read error, got %v", err)
	}
}
