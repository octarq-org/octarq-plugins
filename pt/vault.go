package pt

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
)

type storedSecret struct {
	Login  string `json:"login,omitempty"`
	Cookie string `json:"cookie,omitempty"`
	APIKey string `json:"site_api_token,omitempty"`
	Token  string `json:"token,omitempty"`
}

func (s storedSecret) empty() bool {
	return s.Login == "" && s.Cookie == "" && s.APIKey == "" && s.Token == ""
}

func credentialSubject(secretID string) string {
	return "pt.tracker." + secretID
}

func (p *Plugin) vault() (plugin.VaultService, error) {
	p.mu.RLock()
	vs := p.vaultSvc
	ctx := p.ctx
	p.mu.RUnlock()
	if vs != nil {
		return vs, nil
	}
	if svc, ok := plugin.LookupAs[plugin.VaultService](ctx, plugin.ServiceVault); ok && svc != nil {
		return svc, nil
	}
	return nil, vaultMissing("vault")
}

func secretPayload(s storedSecret) map[string]any {
	out := make(map[string]any, 4)
	if s.Login != "" {
		out["login"] = s.Login
	}
	if s.Cookie != "" {
		out["cookie"] = s.Cookie
	}
	if s.APIKey != "" {
		out["site_api_token"] = s.APIKey
	}
	if s.Token != "" {
		out["token"] = s.Token
	}
	return out
}

func secretFromPayload(payload map[string]any) storedSecret {
	if len(payload) == 0 {
		return storedSecret{}
	}
	str := func(k string) string {
		v, _ := payload[k].(string)
		return v
	}
	return storedSecret{
		Login:  str("login"),
		Cookie: str("cookie"),
		APIKey: str("site_api_token"),
		Token:  str("token"),
	}
}

func verifyUserSecret(meta *plugin.VaultItemMeta, ownerUserID uint) error {
	if meta == nil || ownerUserID == 0 ||
		meta.Scope != plugin.VaultScopeUser || meta.OwnerUserID != ownerUserID {
		return ErrNotFound
	}
	return nil
}

func (p *Plugin) vaultMetaOf(ctx context.Context, t *PTTracker) *plugin.VaultItemMeta {
	if t.VaultItemID == 0 {
		return nil
	}
	vs, err := p.vault()
	if err != nil {
		return nil
	}
	meta, err := vs.GetMeta(ctx, t.OrgID, t.VaultItemID)
	if err != nil {
		return nil
	}
	if verifyUserSecret(meta, t.OwnerUserID) != nil {
		return nil
	}
	return meta
}

func (p *Plugin) storeVaultSecret(ctx context.Context, t *PTTracker, secret storedSecret) error {
	if t.OwnerUserID == 0 {
		return errOwnerRequired
	}
	vs, err := p.vault()
	if err != nil {
		return err
	}
	payload := secretPayload(secret)
	if len(payload) == 0 {
		return nil
	}
	now := p.clock()
	metaMap := map[string]any{}
	if secret.Cookie != "" {
		metaMap["cookie_set_at"] = now.UTC().Format(time.RFC3339)
		t.CookieRefreshedAt = &now
	}
	if t.LastLoginAt != nil {
		metaMap["last_login_at"] = t.LastLoginAt.UTC().Format(time.RFC3339)
	}

	if t.VaultItemID > 0 {
		existing, err := vs.GetMeta(ctx, t.OrgID, t.VaultItemID)
		switch {
		case errors.Is(err, plugin.ErrSecretNotFound):
			t.VaultItemID = 0
		case err != nil:
			return err
		default:
			if err := verifyUserSecret(existing, t.OwnerUserID); err != nil {
				return err
			}
		}
	}

	var meta *plugin.VaultItemMeta
	if t.VaultItemID > 0 {
		meta, err = vs.UpdateSecret(ctx, t.OrgID, t.VaultItemID, &t.Name, metaMap, payload, nil, "Updated tracker credential", t.OwnerUserID)
		if errors.Is(err, plugin.ErrSecretNotFound) {
			t.VaultItemID = 0
		} else if err != nil {
			return err
		}
	}
	if t.VaultItemID == 0 {
		meta, err = vs.PutSecret(ctx, plugin.PutSecretRequest{
			OrgID:          t.OrgID,
			Scope:          plugin.VaultScopeUser,
			OwnerUserID:    t.OwnerUserID,
			CollectionSlug: "default",
			ItemType:       "tracker_credential",
			ProviderType:   "tracker",
			Provider:       "tracker_credential",
			KeyName:        credentialSubject(t.SecretID),
			Name:           t.Name,
			Metadata:       metaMap,
			Payload:        payload,
			ActorUserID:    t.OwnerUserID,
		})
		if err != nil {
			return err
		}
		t.VaultItemID = meta.ID
	}

	updates := map[string]any{
		"vault_item_id": t.VaultItemID,
	}
	if t.CookieRefreshedAt != nil {
		updates["cookie_refreshed_at"] = *t.CookieRefreshedAt
	}
	if t.LastLoginAt != nil {
		updates["last_login_at"] = *t.LastLoginAt
	}
	p.patchTracker(ctx, t, updates)
	return nil
}

func (p *Plugin) readVaultSecret(ctx context.Context, t *PTTracker) (storedSecret, error) {
	if t.VaultItemID == 0 {
		return storedSecret{}, nil
	}
	vs, err := p.vault()
	if err != nil {
		return storedSecret{}, err
	}
	sec, err := vs.GetSecret(ctx, t.OrgID, t.VaultItemID)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			return storedSecret{}, nil
		}
		return storedSecret{}, err
	}
	if err := verifyUserSecret(&sec.Meta, t.OwnerUserID); err != nil {
		return storedSecret{}, err
	}
	return secretFromPayload(sec.Payload), nil
}

func (p *Plugin) shredSecret(ctx context.Context, orgID, vaultItemID, ownerUserID uint) error {
	if orgID == 0 || vaultItemID == 0 {
		return nil
	}
	vs, err := p.vault()
	if err != nil {
		return err
	}
	meta, err := vs.GetMeta(ctx, orgID, vaultItemID)
	if errors.Is(err, plugin.ErrSecretNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := verifyUserSecret(meta, ownerUserID); err != nil {
		return err
	}
	err = vs.DeleteSecret(ctx, orgID, vaultItemID)
	if errors.Is(err, plugin.ErrSecretNotFound) {
		return nil
	}
	return err
}

func credentialViewFromMeta(t *PTTracker, meta *plugin.VaultItemMeta) credentialView {
	if meta == nil {
		return credentialView{}
	}
	view := credentialView{
		HasLoginSecret: slices.Contains(meta.FieldKeys, "login"),
		HasCookie:      slices.Contains(meta.FieldKeys, "cookie"),
		HasAPIKey:      slices.Contains(meta.FieldKeys, "site_api_token"),
		SecretVersion:  meta.CurrentVersion,
		CookieSetAt:    t.CookieRefreshedAt,
		LastLoginAt:    t.LastLoginAt,
	}
	if view.CookieSetAt == nil {
		if raw, ok := meta.Metadata["cookie_set_at"].(string); ok && raw != "" {
			if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
				view.CookieSetAt = &parsed
			}
		}
	}
	if view.LastLoginAt == nil {
		if raw, ok := meta.Metadata["last_login_at"].(string); ok && raw != "" {
			if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
				view.LastLoginAt = &parsed
			}
		}
	}
	return view
}
