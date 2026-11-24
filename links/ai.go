package links

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq/server/llmprovider"
)

const linkAITimeout = 60 * time.Second

type LinkAIStatusInput struct {
	Ctx huma.Context `hidden:"true"`
}

func (i *LinkAIStatusInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type LinkAIStatusOutputBody struct {
	Configured bool   `json:"configured"`
	Provider   string `json:"provider"`
}

type LinkAIStatusOutput struct {
	Body LinkAIStatusOutputBody
}

type LinkAISuggestSlugInputBody struct {
	Target string `json:"target"`
	Title  string `json:"title,omitempty"`
}

type LinkAISuggestSlugInput struct {
	Ctx  huma.Context `hidden:"true"`
	Body LinkAISuggestSlugInputBody
}

func (i *LinkAISuggestSlugInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type LinkAISuggestSlugOutputBody struct {
	Slugs []string `json:"slugs"`
}

type LinkAISuggestSlugOutput struct {
	Body LinkAISuggestSlugOutputBody
}

func (p *Plugin) currentOrgID(r *http.Request) uint {
	if r == nil || p.auth.OrgID == nil {
		return 0
	}
	return p.orgID(r)
}

func (p *Plugin) linksAIStatus(ctx context.Context, input *LinkAIStatusInput) (*LinkAIStatusOutput, error) {
	out := &LinkAIStatusOutput{}
	var orgID uint
	if input != nil && input.Ctx != nil {
		if r, _ := humago.Unwrap(input.Ctx); r != nil {
			orgID = p.currentOrgID(r)
		}
	}
	if p.ctx == nil || p.ctx.LLMProviderForOrg == nil || orgID == 0 {
		return out, nil
	}
	prov, err := p.ctx.LLMProviderForOrg(orgID)
	if err != nil || prov == nil {
		return out, nil
	}
	out.Body.Configured = true
	out.Body.Provider = prov.Name()
	return out, nil
}

func (p *Plugin) suggestLinkSlug(ctx context.Context, input *LinkAISuggestSlugInput) (*LinkAISuggestSlugOutput, error) {
	if input == nil || input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if r == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r = r.WithContext(ctx)
	orgID := p.currentOrgID(r)
	if orgID == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	target := strings.TrimSpace(input.Body.Target)
	if target == "" {
		return nil, huma.Error400BadRequest("Target URL is required. Provide a valid destination URL.")
	}
	if p.ctx == nil || p.ctx.LLMProviderForOrg == nil {
		return nil, huma.Error400BadRequest("AI is not configured. Add an LLM provider in Workspace → LLM Gateway.")
	}
	prov, err := p.ctx.LLMProviderForOrg(orgID)
	if err != nil || prov == nil {
		return nil, huma.Error400BadRequest("AI is not configured. Add an LLM provider in Workspace → LLM Gateway.")
	}

	prompt := "Suggest short-link slugs for this destination.\nURL: " + target
	if input.Body.Title != "" {
		prompt += "\nPage title: " + input.Body.Title
	}
	ctxCtx, cancel := context.WithTimeout(r.Context(), linkAITimeout)
	defer cancel()
	resp, err := prov.Complete(ctxCtx, llmprovider.Request{
		Model: prov.CheapModel(),
		System: "You generate URL slugs. Reply with ONLY a JSON array of 3 to 5 strings. " +
			"Each slug is lowercase, 3-30 chars, [a-z0-9-] only, memorable, and reflects the page content.",
		Messages:  []llmprovider.Message{{Role: llmprovider.RoleUser, Content: prompt}},
		MaxTokens: 200,
		JSON:      true,
	})
	if err != nil {
		return nil, huma.NewError(http.StatusBadGateway, "Could not complete AI request: "+err.Error()+". Retry or check AI configuration.")
	}
	slugs := parseSlugList(resp.Text)
	if len(slugs) == 0 {
		return nil, huma.NewError(http.StatusBadGateway, "No usable slugs generated. Retry with a different title or target.")
	}
	out := &LinkAISuggestSlugOutput{}
	out.Body.Slugs = slugs
	return out, nil
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func parseSlugList(text string) []string {
	text = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(text), "```json"), "```")
	text = strings.TrimSuffix(text, "```")

	var arr []string
	if start, end := strings.Index(text, "["), strings.LastIndex(text, "]"); start >= 0 && end > start {
		_ = json.Unmarshal([]byte(text[start:end+1]), &arr)
	}
	if len(arr) == 0 {
		for _, line := range strings.Split(text, "\n") {
			arr = append(arr, strings.Trim(strings.TrimSpace(line), `-*"',`))
		}
	}
	out := make([]string, 0, 5)
	for _, s := range arr {
		s = strings.ToLower(strings.TrimSpace(s))
		if slugRe.MatchString(s) && len(s) >= 3 && len(s) <= 30 {
			out = append(out, s)
		}
		if len(out) == 5 {
			break
		}
	}
	return out
}
