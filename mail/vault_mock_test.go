package mail

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
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
	listErr error
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
	return m.putInternal(req, plugin.VaultScopeOrg)
}

func (m *mockVaultService) PutInstanceSecret(_ context.Context, req plugin.PutSecretRequest) (*plugin.VaultItemMeta, error) {
	if m.putErr != nil {
		return nil, m.putErr
	}
	req.OrgID = 0
	return m.putInternal(req, plugin.VaultScopeInstance)
}

func (m *mockVaultService) putInternal(req plugin.PutSecretRequest, scope plugin.VaultScope) (*plugin.VaultItemMeta, error) {
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
		Scope:          scope,
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

func (m *mockVaultService) ResolveInstanceSecret(_ context.Context, keyName string) (*plugin.VaultSecret, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ent := range m.items {
		if ent.meta.Scope == plugin.VaultScopeInstance && ent.meta.OrgID == 0 && ent.meta.KeyName == keyName {
			return &plugin.VaultSecret{
				Meta:    ent.meta,
				Version: ent.meta.CurrentVersion,
				Payload: maps.Clone(ent.payload),
			}, nil
		}
	}
	return nil, plugin.ErrSecretNotFound
}

func (m *mockVaultService) ListItems(_ context.Context, orgID uint, filter plugin.VaultFilter) ([]plugin.VaultItemMeta, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	scope := filter.Scope
	if scope == "" {
		scope = plugin.VaultScopeOrg
	}
	if scope == plugin.VaultScopeInstance {
		orgID = 0
	}
	var out []plugin.VaultItemMeta
	for _, ent := range m.items {
		if ent.meta.OrgID != orgID || ent.meta.Scope != scope {
			continue
		}
		if filter.ProviderType != "" && ent.meta.ProviderType != strings.ToLower(filter.ProviderType) {
			continue
		}
		if filter.Provider != "" && ent.meta.Provider != strings.ToLower(filter.Provider) {
			continue
		}
		out = append(out, ent.meta)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDefault != out[j].IsDefault {
			return out[i].IsDefault
		}
		return out[i].ID < out[j].ID
	})
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

// seedVaultSMTPSender seeds an SMTPSender row backed by a VaultService secret.
func seedVaultSMTPSender(p *Plugin, s SMTPSender, pass string) (SMTPSender, error) {
	vs, err := p.vault()
	if err != nil {
		return s, err
	}
	req := plugin.PutSecretRequest{
		OrgID:          s.OrgID,
		CollectionSlug: "integrations",
		ItemType:       "integration",
		ProviderType:   "communication",
		Provider:       "smtp",
		Name:           s.Name,
		Payload: map[string]any{
			"smtpHost":  s.Host,
			"smtpPort":  s.Port,
			"apiKey":    s.User,
			"secretKey": pass,
			"fromEmail": s.FromEmail,
		},
	}
	var meta *plugin.VaultItemMeta
	if s.OrgID == 0 {
		req.Scope = plugin.VaultScopeInstance
		req.KeyName = fmt.Sprintf("mail.system.%s.%d", s.Name, time.Now().UnixNano())
		meta, err = vs.PutInstanceSecret(context.Background(), req)
	} else {
		req.Scope = plugin.VaultScopeOrg
		meta, err = vs.PutSecret(context.Background(), req)
	}
	if err != nil {
		return s, err
	}
	s.VaultItemID = meta.ID
	if err := p.db.Create(&s).Error; err != nil {
		return s, err
	}
	return s, nil
}
