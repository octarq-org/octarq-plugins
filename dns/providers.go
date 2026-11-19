package dns

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq/server/plugin"
)

func (p *Plugin) actorUserID(r *http.Request) uint {
	if p.host != nil && p.host.Session() != nil && r != nil {
		return p.host.Session().UserID(r)
	}
	return 0
}

type providerAccountDTO struct {
	Name   string         `json:"name,omitempty"`
	Type   string         `json:"type,omitempty"`
	Config map[string]any `json:"config,omitempty"`
}

type ListProviderAccountsInput struct {
	Ctx huma.Context `hidden:"true"`
}

func (i *ListProviderAccountsInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type ListProviderAccountsOutput struct {
	Body []ProviderAccount
}

func (p *Plugin) listProviderAccounts(ctx context.Context, input *ListProviderAccountsInput) (*ListProviderAccountsOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if p.orgID(r) == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	var accounts []ProviderAccount
	p.orgDB(r).Order("created_at DESC").Find(&accounts)
	for i := range accounts {
		accounts[i].HasCredentials = accounts[i].VaultItemID > 0
	}
	return &ListProviderAccountsOutput{Body: accounts}, nil
}

type CreateProviderAccountInput struct {
	Ctx  huma.Context `hidden:"true"`
	Body providerAccountDTO
}

func (i *CreateProviderAccountInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type CreateProviderAccountOutput struct {
	Body ProviderAccount
}

func (p *Plugin) createProviderAccount(ctx context.Context, input *CreateProviderAccountInput) (*CreateProviderAccountOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	orgID := p.orgID(r)
	if orgID == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	if !p.hasRole(r, "admin") {
		return nil, huma.Error403Forbidden("Admin role required to create provider account. Contact a workspace admin for access.")
	}
	name := strings.TrimSpace(input.Body.Name)
	typ := strings.TrimSpace(input.Body.Type)
	if name == "" || typ == "" {
		return nil, huma.Error400BadRequest("Provider account name and type are required")
	}

	var vaultItemID uint
	if len(input.Body.Config) > 0 {
		vs, err := p.vault()
		if err != nil {
			return nil, huma.Error500InternalServerError("Credential vault is unavailable. Try again.")
		}
		meta, err := vs.PutSecret(ctx, plugin.PutSecretRequest{
			OrgID:          orgID,
			Scope:          plugin.VaultScopeOrg,
			CollectionSlug: "integrations",
			ItemType:       "integration",
			ProviderType:   "dns",
			Provider:       typ,
			Name:           name,
			Payload:        input.Body.Config,
			ActorUserID:    p.actorUserID(r),
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("Could not store provider configuration in vault. Try again.")
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
		if vaultItemID > 0 {
			if vs, vErr := p.vault(); vErr == nil {
				_ = vs.DeleteSecret(ctx, orgID, vaultItemID)
			}
		}
		return nil, huma.Error500InternalServerError("Could not connect to DNS provider. Check credentials and try again.")
	}
	p.audit(r, "provider.create", "provider", acc.ID, map[string]any{"name": acc.Name, "type": acc.Type})
	acc.HasCredentials = acc.VaultItemID > 0
	return &CreateProviderAccountOutput{Body: acc}, nil
}

type UpdateProviderAccountInput struct {
	Ctx  huma.Context `hidden:"true"`
	ID   uint         `path:"id"`
	Body providerAccountDTO
}

func (i *UpdateProviderAccountInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type UpdateProviderAccountOutput struct {
	Body ProviderAccount
}

func (p *Plugin) updateProviderAccount(ctx context.Context, input *UpdateProviderAccountInput) (*UpdateProviderAccountOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	orgID := p.orgID(r)
	if orgID == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	if !p.hasRole(r, "admin") {
		return nil, huma.Error403Forbidden("Admin role required to update provider account. Contact a workspace admin for access.")
	}
	var acc ProviderAccount
	if p.db.Where("id = ? AND owner_id = ?", input.ID, orgID).First(&acc).Error != nil {
		return nil, huma.Error404NotFound("Provider account not found")
	}
	if strings.TrimSpace(input.Body.Name) != "" {
		acc.Name = strings.TrimSpace(input.Body.Name)
	}
	actorID := p.actorUserID(r)
	if len(input.Body.Config) > 0 {
		vs, err := p.vault()
		if err != nil {
			return nil, huma.Error500InternalServerError("Credential vault is unavailable. Try again.")
		}
		if acc.VaultItemID > 0 {
			if _, err := vs.UpdateSecret(ctx, orgID, acc.VaultItemID, &acc.Name, nil, input.Body.Config, nil, "Updated DNS provider credentials", actorID); err != nil {
				return nil, huma.Error500InternalServerError("Could not update provider configuration in vault. Try again.")
			}
		} else {
			meta, err := vs.PutSecret(ctx, plugin.PutSecretRequest{
				OrgID:          orgID,
				Scope:          plugin.VaultScopeOrg,
				CollectionSlug: "integrations",
				ItemType:       "integration",
				ProviderType:   "dns",
				Provider:       acc.Type,
				Name:           acc.Name,
				Payload:        input.Body.Config,
				ActorUserID:    actorID,
			})
			if err != nil {
				return nil, huma.Error500InternalServerError("Could not store provider configuration in vault. Try again.")
			}
			acc.VaultItemID = meta.ID
		}
	} else if strings.TrimSpace(input.Body.Name) != "" && acc.VaultItemID > 0 {
		if vs, err := p.vault(); err == nil {
			_, _ = vs.UpdateSecret(ctx, orgID, acc.VaultItemID, &acc.Name, nil, nil, nil, "Renamed DNS provider account", actorID)
		}
	}
	p.db.Save(&acc)
	meta := make(map[string]any)
	if strings.TrimSpace(input.Body.Name) != "" {
		meta["name"] = acc.Name
	}
	if len(input.Body.Config) > 0 {
		redactedConfig := make(map[string]string)
		for k := range input.Body.Config {
			redactedConfig[k] = "[REDACTED]"
		}
		meta["config"] = redactedConfig
	}
	p.audit(r, "provider.update", "provider", acc.ID, meta)
	acc.HasCredentials = acc.VaultItemID > 0
	return &UpdateProviderAccountOutput{Body: acc}, nil
}

type DeleteProviderAccountInput struct {
	Ctx huma.Context `hidden:"true"`
	ID  uint         `path:"id"`
}

func (i *DeleteProviderAccountInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type DeleteProviderAccountOutput struct {
	Body map[string]bool
}

func (p *Plugin) deleteProviderAccount(ctx context.Context, input *DeleteProviderAccountInput) (*DeleteProviderAccountOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	orgID := p.orgID(r)
	if orgID == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	if !p.hasRole(r, "admin") {
		return nil, huma.Error403Forbidden("Admin role required to delete provider account. Contact a workspace admin for access.")
	}

	// Verify ownership before anything else: without this, the domain-usage
	// count below runs on a raw ID and its 409-vs-404 response becomes a
	// cross-tenant oracle (it reveals whether another org's provider account
	// exists and is referenced). Fail closed with 404 if the caller doesn't own it.
	var acc ProviderAccount
	if err := p.db.Where("id = ? AND owner_id = ?", input.ID, orgID).First(&acc).Error; err != nil {
		return nil, huma.Error404NotFound("Provider account not found")
	}

	var count int64
	p.db.Model(&Domain{}).Where("provider_account_id = ?", input.ID).Count(&count)
	if count > 0 {
		return nil, huma.NewError(http.StatusConflict, "Cannot delete provider account because it is used by one or more domains. Reassign or delete those domains first.")
	}

	if res := p.db.Where("id = ? AND owner_id = ?", input.ID, orgID).Delete(&ProviderAccount{}); res.RowsAffected == 0 {
		return nil, huma.Error404NotFound("Provider account not found")
	}
	if acc.VaultItemID > 0 {
		if vs, err := p.vault(); err == nil {
			_ = vs.DeleteSecret(ctx, orgID, acc.VaultItemID)
		}
	}
	p.audit(r, "provider.delete", "provider", input.ID, nil)
	return &DeleteProviderAccountOutput{Body: map[string]bool{"ok": true}}, nil
}
