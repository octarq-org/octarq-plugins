package twofa

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/octarq-org/octarq/server/plugin"
)

type getCodeInput struct {
	AccountName string `json:"account_name"`
}

type listAccountsInput struct {
	Tag string `json:"tag,omitempty"`
}

type verifyCodeInput struct {
	AccountName string `json:"account_name"`
	Code        string `json:"code"`
}

// RegisterMCP exposes the 2FA Vault tools to connected AI agents (Cursor, Claude Code, etc.),
// allowing autonomous agents to query 2FA codes during automated workflows.
func (p *Plugin) RegisterMCP(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_2fa_code",
		Description: "Generate the current live 6-digit TOTP two-factor authentication code for a registered account (e.g. AWS, GitHub, Cloudflare).",
	}, p.mcpGetCode)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_2fa_accounts",
		Description: "List all 2FA accounts registered in the workspace vault (name, issuer, tags). Does not reveal secrets or live codes.",
	}, p.mcpListAccounts)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "verify_2fa_code",
		Description: "Verify whether a TOTP code is currently valid for a given 2FA account in the workspace.",
	}, p.mcpVerifyCode)
}

func (p *Plugin) mcpGetCode(ctx context.Context, _ *mcp.CallToolRequest, in getCodeInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.AccountName) == "" {
		return nil, nil, fmt.Errorf("account_name is required")
	}

	orgID := plugin.OrgIDFromContext(ctx)
	if orgID == 0 {
		return nil, nil, fmt.Errorf("missing tenant context: unauthorized")
	}

	code, rem, err := p.GetCode(ctx, orgID, in.AccountName)
	if err != nil {
		return nil, nil, err
	}

	p.logAudit(ctx, orgID, 0, in.AccountName, "mcp_get_code", "mcp_agent", "mcp")

	res := map[string]any{
		"account":           in.AccountName,
		"code":              code,
		"seconds_remaining": rem,
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Current 2FA code for %q: %s (expires in %d seconds)", in.AccountName, code, rem),
		}},
	}, res, nil
}

func (p *Plugin) mcpListAccounts(ctx context.Context, _ *mcp.CallToolRequest, in listAccountsInput) (*mcp.CallToolResult, any, error) {
	orgID := plugin.OrgIDFromContext(ctx)
	if orgID == 0 {
		return nil, nil, fmt.Errorf("missing tenant context: unauthorized")
	}

	if p.ctx == nil || p.ctx.DB == nil {
		return nil, nil, fmt.Errorf("database unavailable")
	}

	var accounts []TwoFAAccount
	query := p.ctx.DB.WithContext(ctx).Where("org_id = ?", orgID)
	if in.Tag != "" {
		query = query.Where("tags LIKE ?", "%"+in.Tag+"%")
	}
	query.Order("pinned DESC, name ASC").Find(&accounts)

	type mcpAccountItem struct {
		ID      uint   `json:"id"`
		Name    string `json:"name"`
		Issuer  string `json:"issuer"`
		Account string `json:"account"`
		Tags    string `json:"tags"`
		Pinned  bool   `json:"pinned"`
	}

	items := make([]mcpAccountItem, len(accounts))
	for i, a := range accounts {
		items[i] = mcpAccountItem{
			ID:      a.ID,
			Name:    a.Name,
			Issuer:  a.Issuer,
			Account: a.Account,
			Tags:    a.Tags,
			Pinned:  a.Pinned,
		}
	}

	buf, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(buf)}},
	}, items, nil
}

func (p *Plugin) mcpVerifyCode(ctx context.Context, _ *mcp.CallToolRequest, in verifyCodeInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.AccountName) == "" || strings.TrimSpace(in.Code) == "" {
		return nil, nil, fmt.Errorf("account_name and code are required")
	}

	orgID := plugin.OrgIDFromContext(ctx)
	if orgID == 0 {
		return nil, nil, fmt.Errorf("missing tenant context: unauthorized")
	}

	valid, err := p.VerifyCode(ctx, orgID, in.AccountName, in.Code)
	if err != nil {
		return nil, nil, err
	}

	p.logAudit(ctx, orgID, 0, in.AccountName, "mcp_verify_code", "mcp_agent", "mcp")

	msg := fmt.Sprintf("Code %s is INVALID for account %q", in.Code, in.AccountName)
	if valid {
		msg = fmt.Sprintf("Code %s is VALID for account %q", in.Code, in.AccountName)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, map[string]bool{"valid": valid}, nil
}
