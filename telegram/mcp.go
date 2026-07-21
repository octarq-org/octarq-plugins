package telegram

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/octarq-org/octarq/plugin"
)

// sendInput is the argument schema for the send_telegram MCP tool.
type sendInput struct {
	Message string `json:"message"`
}

// RegisterMCP exposes this plugin's tools to every connected AI agent — the
// whole point of Octarq being agent-native: implement one optional interface and
// your feature is drivable by Claude Code / Cursor with no extra plumbing.
func (p *Plugin) RegisterMCP(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "send_telegram",
		Description: "Send a message to the workspace's configured Telegram chat. Use it to notify the operator (e.g. an alert or a summary).",
	}, p.mcpSend)
}

func (p *Plugin) mcpSend(ctx context.Context, _ *mcp.CallToolRequest, in sendInput) (*mcp.CallToolResult, any, error) {
	if in.Message == "" {
		return nil, nil, fmt.Errorf("message is required")
	}
	orgID := plugin.OrgIDFromContext(ctx)
	if orgID == 0 {
		orgID = 1
	}
	if token, chatID := p.creds(orgID); token == "" || chatID == "" {
		return nil, nil, fmt.Errorf("telegram is not configured for this workspace; set a bot token and chat id in Settings first")
	}
	if err := p.send(ctx, orgID, in.Message); err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "message sent to Telegram"}},
	}, map[string]bool{"sent": true}, nil
}
