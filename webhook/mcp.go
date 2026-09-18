package webhook

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/octarq-org/octarq/server/plugin"
)

// sendInput is the argument schema for the send_webhook MCP tool.
type sendInput struct {
	Message string `json:"message"`
}

// RegisterMCP exposes this plugin's tools to every connected AI agent — the
// whole point of Octarq being agent-native: implement one optional interface and
// your feature is drivable by Claude Code / Cursor with no extra plumbing.
func (p *Plugin) RegisterMCP(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "send_webhook",
		Description: "Send a message to the workspace's configured Webhook URL. Use it to notify an external service or endpoint.",
	}, p.mcpSend)
}

func (p *Plugin) mcpSend(ctx context.Context, _ *mcp.CallToolRequest, in sendInput) (*mcp.CallToolResult, any, error) {
	if in.Message == "" {
		return nil, nil, fmt.Errorf("message is required")
	}
	orgID := plugin.OrgIDFromContext(ctx)
	if orgID == 0 {
		return nil, nil, fmt.Errorf("missing tenant context: unauthorized")
	}
	if u := p.url(orgID); u == "" {
		return nil, nil, fmt.Errorf("webhook is not configured for this workspace; set a webhook URL in Settings first")
	}
	if err := p.send(ctx, orgID, in.Message); err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "message sent to Webhook"}},
	}, map[string]bool{"sent": true}, nil
}
