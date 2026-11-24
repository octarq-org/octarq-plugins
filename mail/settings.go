package mail

import (
	"context"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type MailSettingsOutputBody struct {
	ReservedMailboxes string `json:"reservedMailboxes"`
	CatchAll          bool   `json:"catchAll"`
	AutoWrapLinks     bool   `json:"autoWrapLinks"`
	SystemSenderID    string `json:"systemSenderId"`
	SystemSenderIDSet bool   `json:"systemSenderIdSet"`
	InboundTokenSet   *bool  `json:"inboundTokenSet,omitempty"`
	OrgSlug           string `json:"orgSlug,omitempty"`
}

type GetMailSettingsInput struct {
	Ctx huma.Context `hidden:"true"`
}

func (i *GetMailSettingsInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type GetMailSettingsOutput struct {
	Body MailSettingsOutputBody
}

func (p *Plugin) getSettings(ctx context.Context, input *GetMailSettingsInput) (*GetMailSettingsOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable; retry the request")
	}
	r, _ := humago.Unwrap(input.Ctx)
	orgID := p.orgID(r)
	if orgID == 0 {
		return nil, huma.Error401Unauthorized("Authentication required; sign in to continue")
	}

	var reservedMailboxes string
	var catchAll bool
	var autoWrapLinks bool
	var systemSenderID string

	if p.getWorkspaceSetting != nil {
		reservedMailboxes = p.getWorkspaceSetting(orgID, "reserved_mailboxes")
		catchAll = p.getWorkspaceSetting(orgID, "catch_all") == "true"
		autoWrapLinks = p.getWorkspaceSetting(orgID, "auto_wrap_links") == "true"
	}
	if p.getGlobalSetting != nil {
		systemSenderID = p.getGlobalSetting("mail_system_sender_id")
	}

	out := &GetMailSettingsOutput{
		Body: MailSettingsOutputBody{
			ReservedMailboxes: reservedMailboxes,
			CatchAll:          catchAll,
			AutoWrapLinks:     autoWrapLinks,
			SystemSenderID:    systemSenderID,
			SystemSenderIDSet: systemSenderID != "",
		},
	}

	if p.db != nil {
		var org orgEntity
		if err := p.db.First(&org, orgID).Error; err == nil {
			out.Body.OrgSlug = org.Slug
			if p.hasRole(r, "admin") {
				set := org.InboundToken != ""
				out.Body.InboundTokenSet = &set
			}
		}
	}

	return out, nil
}

type UpdateMailSettingsInputBody struct {
	ReservedMailboxes *string `json:"reservedMailboxes,omitempty"`
	CatchAll          *bool   `json:"catchAll,omitempty"`
	AutoWrapLinks     *bool   `json:"autoWrapLinks,omitempty"`
	SystemSenderID    *string `json:"systemSenderId,omitempty"`
	InboundToken      *string `json:"inboundToken,omitempty"`
}

type UpdateMailSettingsInput struct {
	Ctx  huma.Context `hidden:"true"`
	Body UpdateMailSettingsInputBody
}

func (i *UpdateMailSettingsInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type UpdateMailSettingsOutput struct {
	Body MailSettingsOutputBody
}

func (p *Plugin) updateSettings(ctx context.Context, input *UpdateMailSettingsInput) (*UpdateMailSettingsOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable; retry the request")
	}
	r, _ := humago.Unwrap(input.Ctx)
	orgID := p.orgID(r)
	if orgID == 0 {
		return nil, huma.Error401Unauthorized("Authentication required; sign in to continue")
	}
	if !p.hasRole(r, "admin") {
		return nil, huma.Error403Forbidden("Admin role required; contact a workspace administrator")
	}

	if input.Body.ReservedMailboxes != nil {
		if p.setWorkspaceSetting == nil {
			return nil, huma.Error500InternalServerError("Settings service unavailable")
		}
		if err := p.setWorkspaceSetting(orgID, "reserved_mailboxes", *input.Body.ReservedMailboxes); err != nil {
			return nil, huma.Error500InternalServerError("Failed to save settings: " + err.Error())
		}
	}

	if input.Body.CatchAll != nil {
		if p.setWorkspaceSetting == nil {
			return nil, huma.Error500InternalServerError("Settings service unavailable")
		}
		val := strconv.FormatBool(*input.Body.CatchAll)
		if err := p.setWorkspaceSetting(orgID, "catch_all", val); err != nil {
			return nil, huma.Error500InternalServerError("Failed to save settings: " + err.Error())
		}
	}

	if input.Body.AutoWrapLinks != nil {
		if p.setWorkspaceSetting == nil {
			return nil, huma.Error500InternalServerError("Settings service unavailable")
		}
		val := strconv.FormatBool(*input.Body.AutoWrapLinks)
		if err := p.setWorkspaceSetting(orgID, "auto_wrap_links", val); err != nil {
			return nil, huma.Error500InternalServerError("Failed to save settings: " + err.Error())
		}
	}

	if input.Body.SystemSenderID != nil {
		if p.setGlobalSetting == nil {
			return nil, huma.Error500InternalServerError("Settings service unavailable")
		}
		if err := p.setGlobalSetting("mail_system_sender_id", *input.Body.SystemSenderID); err != nil {
			return nil, huma.Error500InternalServerError("Failed to save settings: " + err.Error())
		}
	}

	if input.Body.InboundToken != nil && p.db != nil {
		tok := strings.TrimSpace(*input.Body.InboundToken)
		if tok != "" {
			p.db.Table("orgs").Where("id = ?", orgID).Update("inbound_token", tok)
		}
	}

	if p.audit != nil {
		meta := map[string]any{"orgId": orgID}
		if input.Body.CatchAll != nil {
			meta["catchAll"] = *input.Body.CatchAll
		}
		if input.Body.AutoWrapLinks != nil {
			meta["autoWrapLinks"] = *input.Body.AutoWrapLinks
		}
		p.audit(r, "mail.settings.update", "settings", orgID, meta)
	}

	var reservedMailboxes string
	var catchAll bool
	var autoWrapLinks bool
	var systemSenderID string

	if p.getWorkspaceSetting != nil {
		reservedMailboxes = p.getWorkspaceSetting(orgID, "reserved_mailboxes")
		catchAll = p.getWorkspaceSetting(orgID, "catch_all") == "true"
		autoWrapLinks = p.getWorkspaceSetting(orgID, "auto_wrap_links") == "true"
	}
	if p.getGlobalSetting != nil {
		systemSenderID = p.getGlobalSetting("mail_system_sender_id")
	}

	out := &UpdateMailSettingsOutput{
		Body: MailSettingsOutputBody{
			ReservedMailboxes: reservedMailboxes,
			CatchAll:          catchAll,
			AutoWrapLinks:     autoWrapLinks,
			SystemSenderID:    systemSenderID,
			SystemSenderIDSet: systemSenderID != "",
		},
	}

	if p.db != nil {
		var org orgEntity
		if err := p.db.First(&org, orgID).Error; err == nil {
			out.Body.OrgSlug = org.Slug
			set := org.InboundToken != ""
			out.Body.InboundTokenSet = &set
		}
	}

	return out, nil
}
