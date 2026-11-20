package dns

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
)

type mockVaultEntry struct {
	meta    plugin.VaultItemMeta
	payload map[string]any
}

type mockVaultService struct {
	mu      sync.RWMutex
	nextID  uint
	items   map[uint]*mockVaultEntry
	putErr  error
	getErr  error
	metaErr error
}

func newMockVaultService() *mockVaultService {
	return &mockVaultService{
		items: make(map[uint]*mockVaultEntry),
	}
}

func (m *mockVaultService) PutSecret(_ context.Context, req plugin.PutSecretRequest) (*plugin.VaultItemMeta, error) {
	if m.putErr != nil {
		return nil, m.putErr
	}
	if req.OrgID == 0 {
		return nil, errors.New("vault: organization id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	now := time.Now()
	keys := make([]string, 0, len(req.Payload))
	for k := range req.Payload {
		keys = append(keys, k)
	}
	meta := plugin.VaultItemMeta{
		ID:             id,
		SecretID:       fmt.Sprintf("sec-%d", id),
		OrgID:          req.OrgID,
		Scope:          plugin.VaultScopeOrg,
		CollectionSlug: req.CollectionSlug,
		ItemType:       req.ItemType,
		ProviderType:   strings.ToLower(req.ProviderType),
		Provider:       strings.ToLower(req.Provider),
		KeyName:        req.KeyName,
		Name:           req.Name,
		Tags:           append([]string(nil), req.Tags...),
		IsDefault:      req.IsDefault,
		CurrentVersion: 1,
		Configured:     len(req.Payload) > 0,
		FieldKeys:      keys,
		Metadata:       maps.Clone(req.Metadata),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	m.items[id] = &mockVaultEntry{
		meta:    meta,
		payload: maps.Clone(req.Payload),
	}
	cp := meta
	return &cp, nil
}

func (m *mockVaultService) PutInstanceSecret(ctx context.Context, req plugin.PutSecretRequest) (*plugin.VaultItemMeta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockVaultService) UpdateSecret(_ context.Context, orgID, itemID uint, name *string, metadata map[string]any, payload map[string]any, isDefault *bool, changeNote string, actorUserID uint) (*plugin.VaultItemMeta, error) {
	if m.putErr != nil {
		return nil, m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ent, ok := m.items[itemID]
	if !ok || ent.meta.OrgID != orgID {
		return nil, plugin.ErrSecretNotFound
	}
	if name != nil && strings.TrimSpace(*name) != "" {
		ent.meta.Name = strings.TrimSpace(*name)
	}
	if isDefault != nil {
		ent.meta.IsDefault = *isDefault
	}
	if payload != nil {
		if ent.payload == nil {
			ent.payload = make(map[string]any)
		}
		for k, v := range payload {
			ent.payload[k] = v
		}
		ent.meta.CurrentVersion++
	}
	if metadata != nil {
		if ent.meta.Metadata == nil {
			ent.meta.Metadata = make(map[string]any)
		}
		for k, v := range metadata {
			ent.meta.Metadata[k] = v
		}
	}
	ent.meta.UpdatedAt = time.Now()
	cp := ent.meta
	return &cp, nil
}

func (m *mockVaultService) GetMeta(_ context.Context, orgID, itemID uint) (*plugin.VaultItemMeta, error) {
	if m.metaErr != nil {
		return nil, m.metaErr
	}
	if m.getErr != nil {
		return nil, m.getErr
	}
	if orgID == 0 || itemID == 0 {
		return nil, errors.New("vault: invalid org or item id")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ent, ok := m.items[itemID]
	if !ok || ent.meta.OrgID != orgID {
		return nil, plugin.ErrSecretNotFound
	}
	cp := ent.meta
	return &cp, nil
}

func (m *mockVaultService) GetSecret(_ context.Context, orgID, itemID uint) (*plugin.VaultSecret, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if orgID == 0 || itemID == 0 {
		return nil, errors.New("vault: invalid org or item id")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ent, ok := m.items[itemID]
	if !ok || ent.meta.OrgID != orgID {
		return nil, plugin.ErrSecretNotFound
	}
	return &plugin.VaultSecret{
		Meta:    ent.meta,
		Version: ent.meta.CurrentVersion,
		Payload: maps.Clone(ent.payload),
	}, nil
}

func (m *mockVaultService) ResolveByRef(context.Context, uint, string) (*plugin.VaultSecret, error) {
	return nil, plugin.ErrSecretNotFound
}

func (m *mockVaultService) ResolveInstanceSecret(context.Context, string) (*plugin.VaultSecret, error) {
	return nil, plugin.ErrSecretNotFound
}

func (m *mockVaultService) DeleteInstanceSecret(_ context.Context, keyName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, ent := range m.items {
		if ent.meta.Scope == plugin.VaultScopeInstance && ent.meta.KeyName == keyName {
			delete(m.items, id)
			return nil
		}
	}
	return plugin.ErrSecretNotFound
}

func (m *mockVaultService) ListItems(_ context.Context, orgID uint, filter plugin.VaultFilter) ([]plugin.VaultItemMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []plugin.VaultItemMeta
	for _, ent := range m.items {
		if ent.meta.OrgID != orgID {
			continue
		}
		if filter.ProviderType != "" && ent.meta.ProviderType != strings.ToLower(filter.ProviderType) {
			continue
		}
		out = append(out, ent.meta)
	}
	return out, nil
}

func (m *mockVaultService) DeleteSecret(_ context.Context, orgID, itemID uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ent, ok := m.items[itemID]
	if !ok || ent.meta.OrgID != orgID {
		return plugin.ErrSecretNotFound
	}
	delete(m.items, itemID)
	return nil
}

func (m *mockVaultService) ComputeTOTP(context.Context, uint, uint) (string, int, error) {
	return "", 0, errors.New("not supported")
}

func seedVaultProviderAccount(p *Plugin, orgID uint, name, typ string, payload map[string]any) (ProviderAccount, error) {
	vs, err := p.vault()
	if err != nil {
		return ProviderAccount{}, err
	}
	var vaultItemID uint
	if payload != nil {
		meta, err := vs.PutSecret(context.Background(), plugin.PutSecretRequest{
			OrgID:          orgID,
			Scope:          plugin.VaultScopeOrg,
			CollectionSlug: "integrations",
			ItemType:       "integration",
			ProviderType:   "dns",
			Provider:       typ,
			Name:           name,
			Payload:        payload,
		})
		if err != nil {
			return ProviderAccount{}, err
		}
		vaultItemID = meta.ID
	}
	acc := ProviderAccount{
		OrgID:       orgID,
		Name:        name,
		Type:        typ,
		VaultItemID: vaultItemID,
	}
	if err := p.db.Create(&acc).Error; err != nil {
		return ProviderAccount{}, err
	}
	return acc, nil
}
