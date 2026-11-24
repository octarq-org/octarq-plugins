package links

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type LinksSettingsOutputBody struct {
	ReservedSlugs string `json:"reservedSlugs"`
}

type GetLinksSettingsInput struct {
	Ctx huma.Context `hidden:"true"`
}

func (i *GetLinksSettingsInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type GetLinksSettingsOutput struct {
	Body LinksSettingsOutputBody
}

func (p *Plugin) getSettings(ctx context.Context, input *GetLinksSettingsInput) (*GetLinksSettingsOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable; retry the request")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if p.auth.OrgID == nil || p.auth.OrgID(r) == 0 {
		return nil, huma.Error401Unauthorized("Authentication required; sign in to continue")
	}

	var reservedSlugs string
	if p.getGlobalSetting != nil {
		reservedSlugs = p.getGlobalSetting("reserved_slugs")
	}

	return &GetLinksSettingsOutput{
		Body: LinksSettingsOutputBody{
			ReservedSlugs: reservedSlugs,
		},
	}, nil
}

type UpdateLinksSettingsInputBody struct {
	ReservedSlugs string `json:"reservedSlugs"`
}

type UpdateLinksSettingsInput struct {
	Ctx  huma.Context `hidden:"true"`
	Body UpdateLinksSettingsInputBody
}

func (i *UpdateLinksSettingsInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type UpdateLinksSettingsOutput struct {
	Body LinksSettingsOutputBody
}

func (p *Plugin) updateSettings(ctx context.Context, input *UpdateLinksSettingsInput) (*UpdateLinksSettingsOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable; retry the request")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if p.auth.OrgID == nil || p.auth.OrgID(r) == 0 {
		return nil, huma.Error401Unauthorized("Authentication required; sign in to continue")
	}

	isInstAdmin := false
	if p.isInstanceAdmin != nil {
		isInstAdmin = p.isInstanceAdmin(r)
	} else if p.host != nil && p.host.Session() != nil {
		isInstAdmin = p.host.Session().IsInstanceAdmin(r)
	}
	if !isInstAdmin {
		return nil, huma.Error403Forbidden("Instance administrator role required; contact an instance administrator")
	}

	if p.setGlobalSetting == nil && (p.host == nil || p.host.Settings() == nil) {
		return nil, huma.Error500InternalServerError("Settings service unavailable")
	}

	slugs := strings.Join(splitList(input.Body.ReservedSlugs), "\n")
	var err error
	if p.setGlobalSetting != nil {
		err = p.setGlobalSetting("reserved_slugs", slugs)
	} else {
		err = p.host.Settings().SetGlobalSetting("reserved_slugs", slugs)
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to save settings: " + err.Error())
	}

	return &UpdateLinksSettingsOutput{
		Body: LinksSettingsOutputBody{
			ReservedSlugs: slugs,
		},
	}, nil
}
