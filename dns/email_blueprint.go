package dns

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq-plugins/dns/provider"
)

type BlueprintStatus string

const (
	BlueprintStatusOK       BlueprintStatus = "ok"
	BlueprintStatusMissing  BlueprintStatus = "missing"
	BlueprintStatusMismatch BlueprintStatus = "mismatch"
)

type EmailBlueprintRecord struct {
	Type     string          `json:"type"`
	Name     string          `json:"name"`
	Content  string          `json:"content"`
	TTL      int             `json:"ttl"`
	Priority *int            `json:"priority,omitempty"`
	Status   BlueprintStatus `json:"status"`
}

func mxPriority(n int) *int { return &n }

func emailBlueprintRecords() []EmailBlueprintRecord {
	return []EmailBlueprintRecord{
		{Type: "MX", Name: "@", Content: "route1.mx.cloudflare.net", TTL: 1, Priority: mxPriority(10)},
		{Type: "MX", Name: "@", Content: "route2.mx.cloudflare.net", TTL: 1, Priority: mxPriority(53)},
		{Type: "TXT", Name: "@", Content: "v=spf1 include:_spf.mx.cloudflare.net ~all", TTL: 1},
		{Type: "TXT", Name: "_dmarc", Content: "v=DMARC1; p=none; sp=none;", TTL: 1},
	}
}

func normaliseRecordName(name, apex string) string {
	name = strings.TrimSuffix(name, ".")
	apex = strings.TrimSuffix(apex, ".")

	if strings.EqualFold(name, apex) {
		return "@"
	}
	suffix := "." + apex
	if strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix)) {
		return name[:len(name)-len(suffix)]
	}
	return name
}

func computeBlueprintStatus(recs []EmailBlueprintRecord, live []provider.Record, apex string) []EmailBlueprintRecord {
	out := make([]EmailBlueprintRecord, len(recs))
	for i, bp := range recs {
		status := BlueprintStatusMissing
		for _, lr := range live {
			normName := normaliseRecordName(lr.Name, apex)
			if !strings.EqualFold(lr.Type, bp.Type) || !strings.EqualFold(normName, bp.Name) {
				continue
			}
			if strings.TrimSpace(lr.Content) == strings.TrimSpace(bp.Content) {
				status = BlueprintStatusOK
				break
			}
			if status == BlueprintStatusMissing {
				status = BlueprintStatusMismatch
			}
		}
		bp.Status = status
		out[i] = bp
	}
	return out
}

type EmailBlueprintInput struct {
	Ctx huma.Context `hidden:"true"`
	ID  uint         `path:"id"`
}

func (i *EmailBlueprintInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type EmailBlueprintOutput struct {
	Body []EmailBlueprintRecord
}

func (p *Plugin) emailBlueprint(ctx context.Context, input *EmailBlueprintInput) (*EmailBlueprintOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if p.orgID(r) == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}

	var dom Domain
	if err := p.orgDB(r).Where("id = ?", input.ID).First(&dom).Error; err != nil {
		return nil, huma.Error404NotFound("Domain not found")
	}

	blueprint := emailBlueprintRecords()

	if dom.ProviderAccountID == 0 {
		for i := range blueprint {
			blueprint[i].Status = BlueprintStatusMissing
		}
		return &EmailBlueprintOutput{Body: blueprint}, nil
	}

	prov, err := p.providerFor(dom)
	if err != nil {
		return nil, p.providerResolveErr(err)
	}

	live, err := prov.ListRecords(r.Context(), dom.ZoneID)
	if err != nil {
		return nil, p.providerErr("list records", err)
	}

	blueprint = computeBlueprintStatus(blueprint, live, dom.Name)
	return &EmailBlueprintOutput{Body: blueprint}, nil
}

type ApplyEmailBlueprintInput struct {
	Ctx huma.Context `hidden:"true"`
	ID  uint         `path:"id"`
}

func (i *ApplyEmailBlueprintInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type ApplyEmailBlueprintOutput struct {
	Body struct {
		OK      bool `json:"ok"`
		Applied int  `json:"applied"`
		Skipped int  `json:"skipped"`
	}
}

func (p *Plugin) applyEmailBlueprint(ctx context.Context, input *ApplyEmailBlueprintInput) (*ApplyEmailBlueprintOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if p.orgID(r) == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	if !p.hasRole(r, "admin") {
		return nil, huma.Error403Forbidden("Admin role required to apply email blueprint. Contact a workspace admin for access.")
	}

	var dom Domain
	if err := p.orgDB(r).Where("id = ?", input.ID).First(&dom).Error; err != nil {
		return nil, huma.Error404NotFound("Domain not found")
	}

	if dom.ProviderAccountID == 0 {
		return nil, huma.Error400BadRequest("This domain has no DNS provider configured. Connect a provider in domain settings.")
	}

	prov, err := p.providerFor(dom)
	if err != nil {
		return nil, p.providerResolveErr(err)
	}

	live, err := prov.ListRecords(r.Context(), dom.ZoneID)
	if err != nil {
		return nil, p.providerErr("list records", err)
	}

	blueprint := computeBlueprintStatus(emailBlueprintRecords(), live, dom.Name)

	var applied, skipped int
	for _, bp := range blueprint {
		if bp.Status == BlueprintStatusOK {
			skipped++
			continue
		}
		rec := provider.Record{
			Type:     bp.Type,
			Name:     bp.Name,
			Content:  bp.Content,
			TTL:      bp.TTL,
			Priority: bp.Priority,
		}
		if _, err := prov.CreateRecord(r.Context(), dom.ZoneID, rec); err != nil {
			return nil, p.providerErr("create record", err)
		}
		applied++
	}

	orgID := p.orgID(r)
	if p.audit != nil {
		p.audit(r, "email_blueprint_applied", "domain", dom.ID, map[string]any{
			"applied": applied,
			"skipped": skipped,
		})
	}
	if p.publishEvent != nil {
		p.publishEvent(orgID, "domain.email_blueprint_applied", map[string]any{
			"domainId": dom.ID,
			"applied":  applied,
			"skipped":  skipped,
		})
	}

	out := &ApplyEmailBlueprintOutput{}
	out.Body.OK = true
	out.Body.Applied = applied
	out.Body.Skipped = skipped
	return out, nil
}
