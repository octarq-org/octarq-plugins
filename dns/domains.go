package dns

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq/server/plugin"
)

type domainDTO struct {
	Name              string       `json:"name,omitempty"`
	ProviderAccountID uint         `json:"providerAccountId,omitempty"`
	ZoneID            string       `json:"zoneId,omitempty"`
	Note              string       `json:"note,omitempty"`
	ForMail           *bool        `json:"forMail,omitempty"`
	ForLink           *bool        `json:"forLink,omitempty"`
	LinkHosts         *[]hostEntry `json:"linkHosts,omitempty"`
	MailHosts         *[]hostEntry `json:"mailHosts,omitempty"`
}

type ListDomainsInput struct {
	Ctx    huma.Context `hidden:"true"`
	Q      string       `query:"q"`
	Limit  int          `query:"limit"`
	Offset int          `query:"offset"`
}

func (i *ListDomainsInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type ListDomainsOutput struct {
	Body []Domain
}

func (p *Plugin) listDomains(ctx context.Context, input *ListDomainsInput) (*ListDomainsOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if p.orgID(r) == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	var ds []Domain
	q := p.orgDB(r).Order("created_at DESC")
	if input.Q != "" {
		like := "%" + input.Q + "%"
		q = q.Where("name LIKE ? OR note LIKE ?", like, like)
	}
	limit := plugin.PageLimit(input.Limit, 50, 500)
	offset := plugin.PageOffset(input.Offset)
	if base := p.baseDomain(); base != "" {
		q = q.Where(`name <> ? AND name NOT LIKE ? ESCAPE '\'`, base, likeSuffixPattern(base))
	}
	for _, h := range p.sharedPlatformHosts() {
		q = q.Where(`name <> ? AND name NOT LIKE ? ESCAPE '\'`, h, likeSuffixPattern(h))
	}
	q = q.Limit(limit).Offset(offset)
	q.Find(&ds)
	return &ListDomainsOutput{Body: ds}, nil
}

type CreateDomainInput struct {
	Ctx  huma.Context `hidden:"true"`
	Body domainDTO
}

func (i *CreateDomainInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type CreateDomainOutput struct {
	Body Domain
}

func (p *Plugin) createDomain(ctx context.Context, input *CreateDomainInput) (*CreateDomainOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if p.orgID(r) == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	if !p.hasRole(r, "admin") {
		return nil, huma.Error403Forbidden("Admin role required to create domain. Contact a workspace admin for access.")
	}
	name := strings.TrimSpace(strings.ToLower(input.Body.Name))
	if name == "" || input.Body.ProviderAccountID == 0 {
		return nil, huma.Error400BadRequest("Domain name and provider account are required")
	}
	if p.isReservedDomainName(name) {
		return nil, huma.Error400BadRequest("that hostname is reserved and cannot be added")
	}

	owned, ownErr := p.ownsProviderAccount(r, input.Body.ProviderAccountID)
	if ownErr != nil {
		return nil, huma.Error500InternalServerError("Could not verify the provider account. Try again.")
	}
	if !owned {
		return nil, huma.Error404NotFound("Provider account not found")
	}
	var linkHosts, mailHosts []hostEntry
	if input.Body.LinkHosts != nil {
		linkHosts = *input.Body.LinkHosts
	}
	if input.Body.MailHosts != nil {
		mailHosts = *input.Body.MailHosts
	}
	dom := Domain{
		OrgID:             p.orgID(r),
		Name:              name,
		ProviderAccountID: input.Body.ProviderAccountID,
		ZoneID:            input.Body.ZoneID,
		Note:              input.Body.Note,
		LinkHosts:         normalizeHosts(linkHosts),
		MailHosts:         normalizeHosts(mailHosts),
	}
	if input.Body.ForLink != nil {
		dom.ForLink = *input.Body.ForLink
	} else {
		dom.ForLink = len(dom.LinkHosts) > 0
	}
	if input.Body.ForMail != nil {
		dom.ForMail = *input.Body.ForMail
	} else {
		dom.ForMail = len(dom.MailHosts) > 0
	}
	if bad := p.reservedInHostLists(dom.LinkHosts, dom.MailHosts); bad != "" {
		return nil, huma.Error400BadRequest("that hostname is reserved for automatic tenant subdomains")
	}
	if bad := hostsOutsideZone(dom.Name, dom.LinkHosts, dom.MailHosts); bad != "" {
		return nil, huma.Error400BadRequest("host list entry " + bad + " is outside this domain's zone")
	}
	if prov, err := p.providerFor(dom); err == nil && dom.ZoneID != "" {
		if name, err := prov.VerifyZone(r.Context(), dom.ZoneID); err != nil {
			if p.publishEvent != nil {
				p.publishEvent(dom.OrgID, "domain.verify_failed", map[string]any{"name": dom.Name, "zoneId": dom.ZoneID, "error": err.Error()})
			}
			return nil, p.providerErr("verify zone", err)
		} else if dom.Name == "" {
			dom.Name = name
		}
	}
	if err := plugin.CheckQuota(p.ctx, ctx, dom.OrgID, "customDomains", 1); err != nil {
		if errors.Is(err, plugin.ErrQuotaUnavailable) {
			return nil, huma.Error402PaymentRequired("Custom domains are not included in this plan. Upgrade to add custom domains.")
		}
		return nil, huma.Error429TooManyRequests("Custom domain quota exceeded for this workspace. Upgrade your plan to add more domains.")
	}
	if err := p.db.Create(&dom).Error; err != nil {
		return nil, huma.NewError(http.StatusConflict, "Domain already exists in this workspace")
	}
	if err := p.syncDomainHosts(r.Context(), &dom); err != nil {
		p.db.Delete(&dom)
		if errors.Is(err, plugin.ErrHostTaken) {
			return nil, huma.NewError(http.StatusConflict, err.Error())
		}
		return nil, huma.Error400BadRequest(err.Error())
	}
	forgetOrigin(dom.Name)
	p.audit(r, "domain.create", "domain", dom.ID, map[string]any{"name": dom.Name})
	if p.publishEvent != nil {
		p.publishEvent(dom.OrgID, "domain.create", map[string]any{"id": dom.ID, "name": dom.Name})
	}
	return &CreateDomainOutput{Body: dom}, nil
}

type UpdateDomainInput struct {
	Ctx  huma.Context `hidden:"true"`
	ID   uint         `path:"id"`
	Body domainDTO
}

func (i *UpdateDomainInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type UpdateDomainOutput struct {
	Body Domain
}

func (p *Plugin) updateDomain(ctx context.Context, input *UpdateDomainInput) (*UpdateDomainOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if p.orgID(r) == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	if !p.hasRole(r, "admin") {
		return nil, huma.Error403Forbidden("Admin role required to update domain. Contact a workspace admin for access.")
	}
	var dom Domain
	if p.db.Where("id = ? AND owner_id = ?", input.ID, p.orgID(r)).First(&dom).Error != nil {
		return nil, huma.Error404NotFound("Domain not found")
	}
	if input.Body.Note != "" {
		dom.Note = input.Body.Note
	}
	if input.Body.ZoneID != "" {
		dom.ZoneID = input.Body.ZoneID
	}
	if input.Body.ForLink != nil {
		dom.ForLink = *input.Body.ForLink
	}
	if input.Body.ForMail != nil {
		dom.ForMail = *input.Body.ForMail
	}
	if input.Body.LinkHosts != nil {
		dom.LinkHosts = normalizeHosts(*input.Body.LinkHosts)
	}
	if input.Body.MailHosts != nil {
		dom.MailHosts = normalizeHosts(*input.Body.MailHosts)
	}
	if bad := p.reservedInHostLists(dom.LinkHosts, dom.MailHosts); bad != "" {
		return nil, huma.Error400BadRequest("that hostname is reserved for automatic tenant subdomains")
	}
	if bad := hostsOutsideZone(dom.Name, dom.LinkHosts, dom.MailHosts); bad != "" {
		return nil, huma.Error400BadRequest("host list entry " + bad + " is outside this domain's zone")
	}
	if input.Body.ProviderAccountID != 0 {
		owned, ownErr := p.ownsProviderAccount(r, input.Body.ProviderAccountID)
		if ownErr != nil {
			return nil, huma.Error500InternalServerError("Could not verify the provider account. Try again.")
		}
		if !owned {
			return nil, huma.Error404NotFound("Provider account not found")
		}
		dom.ProviderAccountID = input.Body.ProviderAccountID
	}
	if err := p.syncDomainHosts(r.Context(), &dom); err != nil {
		if errors.Is(err, plugin.ErrHostTaken) {
			return nil, huma.NewError(http.StatusConflict, err.Error())
		}
		return nil, huma.Error400BadRequest(err.Error())
	}
	p.db.Save(&dom)
	forgetOrigin(dom.Name)
	p.audit(r, "domain.update", "domain", dom.ID, map[string]any{"name": dom.Name})
	return &UpdateDomainOutput{Body: dom}, nil
}

type DeleteDomainInput struct {
	Ctx huma.Context `hidden:"true"`
	ID  uint         `path:"id"`
}

func (i *DeleteDomainInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type DeleteDomainOutput struct {
	Body map[string]bool
}

func (p *Plugin) deleteDomain(ctx context.Context, input *DeleteDomainInput) (*DeleteDomainOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if p.orgID(r) == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	if !p.hasRole(r, "admin") {
		return nil, huma.Error403Forbidden("Admin role required to delete domain. Contact a workspace admin for access.")
	}
	var dom Domain
	if p.db.Where("id = ? AND owner_id = ?", input.ID, p.orgID(r)).First(&dom).Error != nil {
		return nil, huma.Error404NotFound("Domain not found")
	}
	p.db.Where("domain_id = ? AND owner_id = ?", input.ID, p.orgID(r)).Delete(&DDNSToken{})
	if res := p.db.Where("id = ? AND owner_id = ?", input.ID, p.orgID(r)).Delete(&Domain{}); res.RowsAffected == 0 {
		return nil, huma.Error404NotFound("Domain not found")
	}
	_ = p.removeDomainHosts(r.Context(), dom.OrgID, dom.ID)
	forgetOrigin(dom.Name)
	p.audit(r, "domain.delete", "domain", input.ID, nil)
	return &DeleteDomainOutput{Body: map[string]bool{"ok": true}}, nil
}
