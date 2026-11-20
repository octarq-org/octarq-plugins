package maillink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/octarq-org/octarq/server/plugin"
)

type listInput struct {
	Limit int `json:"limit,omitempty"`
}

func (*Plugin) MCPTier() string { return "" }

func (p *Plugin) RegisterMCP(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_email_links",
		Description: "List short links Octarq auto-created from recently received emails (slug, target URL, subject, sender), newest first.",
	}, p.mcpList)
}

func (p *Plugin) mcpList(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, any, error) {
	orgID := plugin.OrgIDFromContext(ctx)
	if orgID == 0 {
		return nil, nil, errors.New("missing tenant context: unauthorized")
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	out := p.list(ctx, orgID, limit)
	buf, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(buf)}},
	}, out, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
