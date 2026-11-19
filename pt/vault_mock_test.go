package pt

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
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
	mu     sync.RWMutex
	nextID uint
	items  map[uint]*mockVaultEntry
}

func newMockVaultService() *mockVaultService {
	return &mockVaultService{
		items: make(map[uint]*mockVaultEntry),
	}
}

func fieldKeysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (m *mockVaultService) PutSecret(_ context.Context, req plugin.PutSecretRequest) (*plugin.VaultItemMeta, error) {
	if req.OrgID == 0 {
		return nil, errors.New("vault: organization id is required")
	}
	scope := req.Scope
	if scope == "" {
		scope = plugin.VaultScopeOrg
	}
	if scope == plugin.VaultScopeUser && req.OwnerUserID == 0 {
		return nil, errors.New("vault: owner user id is required for user-scoped secrets")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if req.KeyName != "" {
		for _, ent := range m.items {
			if ent.meta.OrgID == req.OrgID && ent.meta.Scope == scope &&
				ent.meta.OwnerUserID == req.OwnerUserID && ent.meta.KeyName == req.KeyName {
				ent.payload = maps.Clone(req.Payload)
				ent.meta.Name = req.Name
				ent.meta.CurrentVersion++
				ent.meta.FieldKeys = fieldKeysOf(ent.payload)
				if req.Metadata != nil {
					if ent.meta.Metadata == nil {
						ent.meta.Metadata = map[string]any{}
					}
					for k, v := range req.Metadata {
						ent.meta.Metadata[k] = v
					}
				}
				ent.meta.UpdatedAt = time.Now()
				cp := ent.meta
				return &cp, nil
			}
		}
	}
	m.nextID++
	id := m.nextID
	now := time.Now()
	meta := plugin.VaultItemMeta{
		ID:             id,
		SecretID:       fmt.Sprintf("sec-%d", id),
		OrgID:          req.OrgID,
		Scope:          scope,
		OwnerUserID:    req.OwnerUserID,
		CollectionSlug: req.CollectionSlug,
		ItemType:       req.ItemType,
		ProviderType:   strings.ToLower(req.ProviderType),
		Provider:       strings.ToLower(req.Provider),
		KeyName:        req.KeyName,
		Name:           req.Name,
		CurrentVersion: 1,
		Configured:     len(req.Payload) > 0,
		FieldKeys:      fieldKeysOf(req.Payload),
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

func (m *mockVaultService) PutInstanceSecret(context.Context, plugin.PutSecretRequest) (*plugin.VaultItemMeta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockVaultService) UpdateSecret(_ context.Context, orgID, itemID uint, name *string, metadata map[string]any, payload map[string]any, isDefault *bool, changeNote string, actorUserID uint) (*plugin.VaultItemMeta, error) {
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
			ent.payload = map[string]any{}
		}
		for k, v := range payload {
			if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
				delete(ent.payload, k)
				continue
			}
			ent.payload[k] = v
		}
		ent.meta.CurrentVersion++
		ent.meta.FieldKeys = fieldKeysOf(ent.payload)
	}
	if metadata != nil {
		if ent.meta.Metadata == nil {
			ent.meta.Metadata = map[string]any{}
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
		if filter.ItemType != "" && ent.meta.ItemType != filter.ItemType {
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
