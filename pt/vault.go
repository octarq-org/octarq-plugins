package pt

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
)

// storedSecret is the decrypted credential payload. It is never logged, never
// returned by an API handler, and never written to a column in plaintext.
type storedSecret struct {
	Login  string `json:"login,omitempty"`
	Cookie string `json:"cookie,omitempty"`
	APIKey string `json:"site_api_token,omitempty"`
	Token  string `json:"token,omitempty"`
}

func (s storedSecret) empty() bool {
	return s.Login == "" && s.Cookie == "" && s.APIKey == "" && s.Token == ""
}

// credentialSubject is the per-tracker KeyName inside VaultService.
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

func (p *Plugin) storeVaultSecret(ctx context.Context, t *PTTracker, secret storedSecret) error {
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

	var meta *plugin.VaultItemMeta
	if t.VaultItemID > 0 {
		meta, err = vs.UpdateSecret(ctx, t.OrgID, t.VaultItemID, &t.Name, metaMap, payload, nil, "Updated tracker credential", t.CreatedByUserID)
		if errors.Is(err, plugin.ErrSecretNotFound) {
			t.VaultItemID = 0
		} else if err != nil {
			return err
		}
	}
	if t.VaultItemID == 0 {
		meta, err = vs.PutSecret(ctx, plugin.PutSecretRequest{
			OrgID:          t.OrgID,
			Scope:          plugin.VaultScopeOrg,
			CollectionSlug: "default",
			ItemType:       "tracker_credential",
			ProviderType:   "tracker",
			Provider:       "tracker_credential",
			KeyName:        credentialSubject(t.SecretID),
			Name:           t.Name,
			Metadata:       metaMap,
			Payload:        payload,
			ActorUserID:    t.CreatedByUserID,
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
	return secretFromPayload(sec.Payload), nil
}

func (p *Plugin) shredSecret(ctx context.Context, orgID, vaultItemID uint) error {
	if orgID == 0 || vaultItemID == 0 {
		return nil
	}
	vs, err := p.vault()
	if err != nil {
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
