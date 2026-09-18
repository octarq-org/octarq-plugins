// Package telegram is an Octarq plugin that bridges your instance to a Telegram
// chat: it forwards inbound email to Telegram, and exposes a `send_telegram`
// MCP tool so AI agents (Claude Code, Cursor, …) can ping you directly.
//
// It is a community connector plugin built entirely on the public plugin.Context
// seams — OnEmail (inbound-mail hook), Notify (core's Telegram sender), the
// per-workspace settings store, and MCPProvider — with no fork and no access to
// octarq internals. Use it as a reference for writing your own connector.
package telegram

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/octarq-org/octarq/server/plugin"
)

// Plugin is composed into a host with app.Use(&telegram.Plugin{}). It keeps a
// reference to the Context so the MCP tool (registered outside Mount) can reach
// the settings store and Notify.
type Plugin struct {
	ctx  *plugin.Context
	host plugin.Host
}

// Per-workspace setting keys. The bot token is stored encrypted at rest.
const (
	keyBotToken = "telegram.botTokenEnc"
	keyChatID   = "telegram.chatId"
)

func (*Plugin) Name() string { return "telegram" }

func (*Plugin) Describe() plugin.Info {
	return plugin.Info{
		Title:       "Telegram",
		Description: "Forward inbound email to a Telegram chat, and let AI agents ping you via an MCP tool.",
	}
}

func (*Plugin) Models() []any { return nil }

// Mount wires the inbound-email hook and the settings/test routes. Every route
// is auto-gated: if the workspace has the Telegram feature disabled, the host
// answers 404 before the handler runs.
func (p *Plugin) Mount(mux plugin.Mux, ctx *plugin.Context) {
	p.ctx = ctx
	p.host = plugin.EnsureHost(ctx)

	// Inbound email → Telegram. The handler runs async in its own goroutine, so
	// it must not block the request path.
	if p.host != nil && p.host.Events() != nil {
		p.host.Events().OnEmail(func(e plugin.EmailEvent) {
			text := "📬 New mail\nTo: " + e.To + "\nFrom: " + e.From + "\nSubject: " + e.Subject
			_ = p.send(context.Background(), e.OrgID, text)
		})
	}

	mux.Handle("GET /api/telegram/settings", ctx.Guard(http.HandlerFunc(p.getSettings)))
	mux.Handle("PUT /api/telegram/settings", ctx.Guard(http.HandlerFunc(p.putSettings)))
	mux.Handle("POST /api/telegram/test", ctx.Guard(http.HandlerFunc(p.testSend)))
}

// send delivers text to the workspace's configured Telegram chat via the core
// Notify channel. It is a no-op (nil error) when the workspace hasn't set up
// Telegram, so callers on the mail path never surface an error to the user.
func (p *Plugin) send(ctx context.Context, orgID uint, text string) error {
	if p.ctx == nil || p.ctx.Notify == nil {
		return nil
	}
	token, chatID := p.creds(orgID)
	if token == "" || chatID == "" {
		return nil
	}
	cfg, err := json.Marshal(map[string]string{"botToken": token, "chatId": chatID})
	if err != nil {
		return err
	}
	return p.ctx.Notify(ctx, "telegram", string(cfg), text)
}

// creds reads and decrypts the workspace's Telegram credentials.
func (p *Plugin) creds(orgID uint) (token, chatID string) {
	if p.host == nil || p.host.Settings() == nil {
		return "", ""
	}
	chatID = p.host.Settings().GetWorkspaceSetting(orgID, keyChatID)
	if enc := p.host.Settings().GetWorkspaceSetting(orgID, keyBotToken); enc != "" && p.host.Crypto() != nil {
		if b, err := p.host.Crypto().Decrypt(enc); err == nil {
			token = string(b)
		}
	}
	return token, chatID
}

type settingsOut struct {
	ChatID   string `json:"chatId"`
	HasToken bool   `json:"hasToken"`
}

func (p *Plugin) getSettings(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	token, chatID := p.creds(orgID)
	writeJSON(w, settingsOut{ChatID: chatID, HasToken: token != ""})
}

type settingsIn struct {
	BotToken string `json:"botToken"` // empty = keep existing
	ChatID   string `json:"chatId"`
}

func (p *Plugin) putSettings(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	var in settingsIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if p.host == nil || p.host.Settings() == nil {
		http.Error(w, "settings unavailable", http.StatusInternalServerError)
		return
	}
	_ = p.host.Settings().SetWorkspaceSetting(orgID, keyChatID, in.ChatID)
	if in.BotToken != "" && p.host.Crypto() != nil {
		if enc, err := p.host.Crypto().Encrypt([]byte(in.BotToken)); err == nil {
			_ = p.host.Settings().SetWorkspaceSetting(orgID, keyBotToken, enc)
		}
	}
	token, chatID := p.creds(orgID)
	writeJSON(w, settingsOut{ChatID: chatID, HasToken: token != ""})
}

func (p *Plugin) testSend(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	if err := p.send(r.Context(), orgID, "✅ Octarq Telegram connector test — you're all set."); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]bool{"sent": true})
}

func (p *Plugin) orgID(r *http.Request) uint {
	if p.host != nil && p.host.Session() != nil {
		return p.host.Session().OrgID(r)
	}
	return 0
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Compile-time assertions for every contract this plugin implements.
var (
	_ plugin.Plugin      = (*Plugin)(nil)
	_ plugin.Describer   = (*Plugin)(nil)
	_ plugin.MCPProvider = (*Plugin)(nil)
)
