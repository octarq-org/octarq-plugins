package mail

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq/server/llmprovider"
)

type MailAIStatusInput struct {
	Ctx huma.Context `hidden:"true"`
}

func (i *MailAIStatusInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type MailAIStatusOutputBody struct {
	Configured bool   `json:"configured"`
	Provider   string `json:"provider"`
}

type MailAIStatusOutput struct {
	Body MailAIStatusOutputBody
}

func (p *Plugin) aiStatus(_ context.Context, input *MailAIStatusInput) (*MailAIStatusOutput, error) {
	out := &MailAIStatusOutput{}
	if input.Ctx == nil || p.ctx == nil || p.ctx.LLMProviderForOrg == nil || p.orgID == nil {
		return out, nil
	}
	r, _ := humago.Unwrap(input.Ctx)
	if r == nil {
		return out, nil
	}
	orgID := p.orgID(r)
	if orgID == 0 {
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

type AISummarizeEmailInput struct {
	Ctx huma.Context `hidden:"true"`
	ID  uint         `path:"id"`
}

func (i *AISummarizeEmailInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type AISummarizeEmailOutputBody struct {
	Summary string `json:"summary"`
}

type AISummarizeEmailOutput struct {
	Body AISummarizeEmailOutputBody
}

func (p *Plugin) aiSummarizeEmail(_ context.Context, input *AISummarizeEmailInput) (*AISummarizeEmailOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("An unexpected error occurred while processing the request context.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	if r == nil || p.orgID == nil || p.orgID(r) == 0 {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	orgID := p.orgID(r)
	if p.ctx == nil || p.ctx.LLMProviderForOrg == nil {
		return nil, huma.Error400BadRequest("AI is not configured. Add an LLM provider in Workspace → LLM Gateway.")
	}
	prov, err := p.ctx.LLMProviderForOrg(orgID)
	if err != nil || prov == nil {
		return nil, huma.Error400BadRequest("AI is not configured. Add an LLM provider in Workspace → LLM Gateway.")
	}

	tdb := p.tenantDB(orgID)
	if tdb == nil {
		return nil, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	orgMailboxes := tdb.Model(&Mailbox{}).Select("id")
	var e Email
	if p.db.Where("id = ? AND mailbox_id IN (?)", input.ID, orgMailboxes).First(&e).Error != nil {
		return nil, huma.Error404NotFound("Email not found. Verify the identifier and try again.")
	}

	body := e.Text
	if strings.TrimSpace(body) == "" {
		body = htmlTagRe.ReplaceAllString(e.HTML, " ")
	}
	const maxBody = 8000
	runes := []rune(body)
	if len(runes) > maxBody {
		body = string(runes[:maxBody])
	}
	content := "From: " + e.FromAddr + "\nSubject: " + e.Subject + "\n\n" + body

	ctxCtx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	resp, err := prov.Complete(ctxCtx, llmprovider.Request{
		Model: prov.CheapModel(),
		System: "Summarize this email in 2-3 sentences, in the same language the email is written in. " +
			"Lead with what it is (bill, verification code, newsletter, personal, ...) and any action or deadline. Plain text only.",
		Messages:  []llmprovider.Message{{Role: llmprovider.RoleUser, Content: content}},
		MaxTokens: 300,
	})
	if err != nil {
		return nil, huma.NewError(http.StatusBadGateway, "Could not complete AI request: "+err.Error()+". Retry or check AI configuration.")
	}
	return &AISummarizeEmailOutput{Body: AISummarizeEmailOutputBody{Summary: strings.TrimSpace(resp.Text)}}, nil
}
