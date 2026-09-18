// Package webhook is an Octarq plugin that bridges your instance to a webhook
// URL: it forwards inbound email to a Webhook, and exposes a `send_webhook`
// MCP tool so AI agents (Claude Code, Cursor, …) can ping external services directly.
//
// It is a community connector plugin built entirely on the public plugin.Context
// seams — OnEmail (inbound-mail hook), Notify (core's Webhook sender), the
// per-workspace settings store, and MCPProvider — with no fork and no access to
// octarq internals. Use it as a reference for writing your own connector.
package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/octarq-org/octarq/server/plugin"
)

// Plugin is composed into a host with app.Use(&webhook.Plugin{}). It keeps a
// reference to the Context so the MCP tool (registered outside Mount) can reach
// the settings store and Notify.
type Plugin struct {
	ctx  *plugin.Context
	host plugin.Host
}

// Per-workspace setting key for the webhook URL.
const (
	keyURL = "webhook.url"
)

func (*Plugin) Name() string { return "webhook" }

func (*Plugin) Describe() plugin.Info {
	return plugin.Info{
		Title:       "Webhook",
		Description: "Forward inbound email to a webhook URL.",
	}
}

func (*Plugin) Models() []any { return nil }

// Mount wires the inbound-email hook and the settings/test routes. Every route
// is auto-gated: if the workspace has the Webhook feature disabled, the host
// answers 404 before the handler runs.
func (p *Plugin) Mount(mux plugin.Mux, ctx *plugin.Context) {
	p.ctx = ctx
	p.host = plugin.EnsureHost(ctx)

	// Inbound email → Webhook. The handler runs async in its own goroutine, so
	// it must not block the request path.
	if p.host != nil && p.host.Events() != nil {
		p.host.Events().OnEmail(func(e plugin.EmailEvent) {
			text := "📬 New mail\nTo: " + e.To + "\nFrom: " + e.From + "\nSubject: " + e.Subject
			_ = p.send(context.Background(), e.OrgID, text)
		})
	}

	mux.Handle("GET /api/webhook/settings", ctx.Guard(http.HandlerFunc(p.getSettings)))
	mux.Handle("PUT /api/webhook/settings", ctx.Guard(http.HandlerFunc(p.putSettings)))
	mux.Handle("POST /api/webhook/test", ctx.Guard(http.HandlerFunc(p.testSend)))
}

// send delivers text to the workspace's configured Webhook URL via the core
// Notify channel. It is a no-op (nil error) when the workspace hasn't set up
// Webhook, so callers on the mail path never surface an error to the user.
func (p *Plugin) send(ctx context.Context, orgID uint, text string) error {
	if p.ctx == nil || p.ctx.Notify == nil {
		return nil
	}
	u := p.url(orgID)
	if u == "" {
		return nil
	}
	cfg, err := json.Marshal(map[string]string{"url": u})
	if err != nil {
		return err
	}
	return p.ctx.Notify(ctx, "webhook", string(cfg), text)
}

// url reads the workspace's Webhook URL setting.
func (p *Plugin) url(orgID uint) string {
	if p.host == nil || p.host.Settings() == nil {
		return ""
	}
	return p.host.Settings().GetWorkspaceSetting(orgID, keyURL)
}

type settingsOut struct {
	URL string `json:"url"`
}

func (p *Plugin) Validate(ctx context.Context, host plugin.Host) error {
	if host == nil {
		return errors.New("webhook: host runtime is required")
	}
	return nil
}

func (p *Plugin) getSettings(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	if orgID == 0 {
		http.Error(w, "unauthorized workspace", http.StatusUnauthorized)
		return
	}
	writeJSON(w, settingsOut{URL: p.url(orgID)})
}

type settingsIn struct {
	URL string `json:"url"`
}

func (p *Plugin) putSettings(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	if orgID == 0 {
		http.Error(w, "unauthorized workspace", http.StatusUnauthorized)
		return
	}
	var in settingsIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if p.host == nil || p.host.Settings() == nil {
		http.Error(w, "settings unavailable", http.StatusInternalServerError)
		return
	}
	if err := p.host.Settings().SetWorkspaceSetting(orgID, keyURL, in.URL); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, settingsOut{URL: p.url(orgID)})
}

func (p *Plugin) testSend(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	if orgID == 0 {
		http.Error(w, "unauthorized workspace", http.StatusUnauthorized)
		return
	}
	if err := p.send(r.Context(), orgID, "✅ Octarq Webhook connector test — you're all set."); err != nil {
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
// Menus contributes the plugin's sidebar entry. It lives here, not on the
// frontend half: UIPlugin.menu was removed from the plugin SDK in 0.8.0, and a
// static frontend menu is ignored even where one is still declared.
func (*Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{ID: "webhook", Label: "Webhook", Path: "/webhook", Icon: "🪝", Category: "Workspace"},
	}
}

var (
	_ plugin.Plugin       = (*Plugin)(nil)
	_ plugin.Validator    = (*Plugin)(nil)
	_ plugin.Describer    = (*Plugin)(nil)
	_ plugin.MCPProvider  = (*Plugin)(nil)
	_ plugin.MenuProvider = (*Plugin)(nil)
)
