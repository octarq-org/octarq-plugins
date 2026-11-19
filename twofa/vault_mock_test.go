package twofa

import (
	"context"
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
		nextID: 1,
		items:  make(map[uint]*mockVaultEntry),
	}
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	return maps.Clone(src)
}

func cloneMeta(m plugin.VaultItemMeta) *plugin.VaultItemMeta {
	cp := m
	cp.Metadata = cloneMap(m.Metadata)
	cp.Tags = slices.Clone(m.Tags)
	cp.FieldKeys = slices.Clone(m.FieldKeys)
	return &cp
}

func (m *mockVaultService) PutSecret(_ context.Context, req plugin.PutSecretRequest) (*plugin.VaultItemMeta, error) {
	if req.OrgID == 0 {
		return nil, fmt.Errorf("orgID required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextID
	m.nextID++
	now := time.Now().UTC()
	scope := req.Scope
	if scope == "" {
		scope = plugin.VaultScopeOrg
	}
	meta := plugin.VaultItemMeta{
		ID:                   id,
		SecretID:             fmt.Sprintf("sec_%d", id),
		OrgID:                req.OrgID,
		Scope:                scope,
		OwnerUserID:          req.OwnerUserID,
		CollectionSlug:       req.CollectionSlug,
		Environment:          req.Environment,
		FolderPath:           req.FolderPath,
		KeyName:              req.KeyName,
		Name:                 req.Name,
		ItemType:             req.ItemType,
		ProviderType:         req.ProviderType,
		Provider:             req.Provider,
		Metadata:             cloneMap(req.Metadata),
		Tags:                 slices.Clone(req.Tags),
		IsDefault:            req.IsDefault,
		CurrentVersion:       1,
		Configured:           true,
		ExpiresAt:            req.ExpiresAt,
		RotationIntervalDays: req.RotationIntervalDays,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	m.items[id] = &mockVaultEntry{
		meta:    meta,
		payload: cloneMap(req.Payload),
	}
	return cloneMeta(meta), nil
}

func (m *mockVaultService) UpdateSecret(_ context.Context, orgID, itemID uint, name *string, metadata map[string]any, payload map[string]any, isDefault *bool, _ string, _ uint) (*plugin.VaultItemMeta, error) {
	if orgID == 0 {
		return nil, plugin.ErrSecretNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.items[itemID]
	if !ok || entry.meta.OrgID != orgID {
		return nil, plugin.ErrSecretNotFound
	}
	if name != nil {
		entry.meta.Name = *name
	}
	if metadata != nil {
		if entry.meta.Metadata == nil {
			entry.meta.Metadata = make(map[string]any)
		}
		for k, v := range metadata {
			entry.meta.Metadata[k] = v
		}
	}
	if payload != nil {
		if entry.payload == nil {
			entry.payload = make(map[string]any)
		}
		for k, v := range payload {
			entry.payload[k] = v
		}
		entry.meta.CurrentVersion++
	}
	if isDefault != nil {
		entry.meta.IsDefault = *isDefault
	}
	entry.meta.UpdatedAt = time.Now().UTC()
	return cloneMeta(entry.meta), nil
}

func (m *mockVaultService) GetMeta(_ context.Context, orgID, itemID uint) (*plugin.VaultItemMeta, error) {
	if orgID == 0 {
		return nil, plugin.ErrSecretNotFound
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.items[itemID]
	if !ok || entry.meta.OrgID != orgID {
		return nil, plugin.ErrSecretNotFound
	}
	return cloneMeta(entry.meta), nil
}

func (m *mockVaultService) GetSecret(_ context.Context, orgID, itemID uint) (*plugin.VaultSecret, error) {
	if orgID == 0 {
		return nil, plugin.ErrSecretNotFound
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.items[itemID]
	if !ok || entry.meta.OrgID != orgID {
		return nil, plugin.ErrSecretNotFound
	}
	return &plugin.VaultSecret{
		Meta:    *cloneMeta(entry.meta),
		Version: entry.meta.CurrentVersion,
		Payload: cloneMap(entry.payload),
	}, nil
}

func (m *mockVaultService) ResolveByRef(_ context.Context, _ uint, _ string) (*plugin.VaultSecret, error) {
	return nil, plugin.ErrSecretNotFound
}

func (m *mockVaultService) ResolveInstanceSecret(_ context.Context, keyName string) (*plugin.VaultSecret, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, entry := range m.items {
		if entry.meta.Scope == plugin.VaultScopeInstance && entry.meta.KeyName == keyName {
			return &plugin.VaultSecret{
				Meta:    *cloneMeta(entry.meta),
				Version: entry.meta.CurrentVersion,
				Payload: cloneMap(entry.payload),
			}, nil
		}
	}
	return nil, plugin.ErrSecretNotFound
}

func (m *mockVaultService) PutInstanceSecret(_ context.Context, req plugin.PutSecretRequest) (*plugin.VaultItemMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextID
	m.nextID++
	now := time.Now().UTC()
	meta := plugin.VaultItemMeta{
		ID:             id,
		SecretID:       fmt.Sprintf("sec_%d", id),
		Scope:          plugin.VaultScopeInstance,
		KeyName:        req.KeyName,
		Name:           req.Name,
		ItemType:       req.ItemType,
		ProviderType:   req.ProviderType,
		Provider:       req.Provider,
		Metadata:       cloneMap(req.Metadata),
		Tags:           slices.Clone(req.Tags),
		IsDefault:      req.IsDefault,
		CurrentVersion: 1,
		Configured:     true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	m.items[id] = &mockVaultEntry{
		meta:    meta,
		payload: cloneMap(req.Payload),
	}
	return cloneMeta(meta), nil
}

func (m *mockVaultService) DeleteInstanceSecret(_ context.Context, keyName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, entry := range m.items {
		if entry.meta.Scope == plugin.VaultScopeInstance && entry.meta.KeyName == keyName {
			delete(m.items, id)
			return nil
		}
	}
	return plugin.ErrSecretNotFound
}

func (m *mockVaultService) ListItems(_ context.Context, orgID uint, filter plugin.VaultFilter) ([]plugin.VaultItemMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]plugin.VaultItemMeta, 0)
	for _, entry := range m.items {
		if filter.Scope != "" && entry.meta.Scope != filter.Scope {
			continue
		}
		if filter.Scope != plugin.VaultScopeInstance && entry.meta.OrgID != orgID {
			continue
		}
		if filter.ItemType != "" && entry.meta.ItemType != filter.ItemType {
			continue
		}
		if filter.ProviderType != "" && entry.meta.ProviderType != filter.ProviderType {
			continue
		}
		if filter.Provider != "" && entry.meta.Provider != filter.Provider {
			continue
		}
		if filter.KeyName != "" && entry.meta.KeyName != filter.KeyName {
			continue
		}
		if filter.OwnerUserID != 0 && entry.meta.OwnerUserID != filter.OwnerUserID {
			continue
		}
		if filter.Query != "" {
			q := strings.ToLower(filter.Query)
			issuer, _ := entry.meta.Metadata["issuer"].(string)
			account, _ := entry.meta.Metadata["account"].(string)
			if !strings.Contains(strings.ToLower(entry.meta.Name), q) &&
				!strings.Contains(strings.ToLower(issuer), q) &&
				!strings.Contains(strings.ToLower(account), q) {
				continue
			}
		}
		out = append(out, *cloneMeta(entry.meta))
	}
	slices.SortFunc(out, func(a, b plugin.VaultItemMeta) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out, nil
}

func (m *mockVaultService) DeleteSecret(_ context.Context, orgID, itemID uint) error {
	if orgID == 0 {
		return plugin.ErrSecretNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.items[itemID]
	if !ok || entry.meta.OrgID != orgID {
		return plugin.ErrSecretNotFound
	}
	delete(m.items, itemID)
	return nil
}

func (m *mockVaultService) ComputeTOTP(_ context.Context, orgID, itemID uint) (string, int, error) {
	if orgID == 0 {
		return "", 0, plugin.ErrSecretNotFound
	}
	m.mu.RLock()
	entry, ok := m.items[itemID]
	if !ok || entry.meta.OrgID != orgID {
		m.mu.RUnlock()
		return "", 0, plugin.ErrSecretNotFound
	}
	payload := cloneMap(entry.payload)
	m.mu.RUnlock()

	secret, _ := payload["secret"].(string)
	period := 30
	digits := 6
	algo := "SHA1"

	if uri, _ := payload["otpauthUri"].(string); uri != "" {
		parsed, parsedSecret, err := ParseOTPAuthURI(uri)
		if err == nil && parsedSecret != "" {
			secret = parsedSecret
			if parsed.Period > 0 {
				period = parsed.Period
			}
			if parsed.Digits > 0 {
				digits = parsed.Digits
			}
			if parsed.Algorithm != "" {
				algo = parsed.Algorithm
			}
		}
	}
	if secret == "" {
		return "", 0, fmt.Errorf("missing TOTP secret")
	}
	now := time.Now()
	code, err := GenerateCode(secret, now, period, digits, algo)
	if err != nil {
		return "", 0, err
	}
	return code, SecondsRemaining(now, period), nil
}

func intFromAny(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	default:
		return 0, false
	}
}
