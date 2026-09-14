// Package myplugin is a starter Octarq plugin — the Go (backend) half of a
// full-stack feature. Rename the package, module path (go.mod), and Name() to
// make it yours.
//
// A plugin has two mirror halves composed into a host at build time, never a
// fork:
//
//   - this Go module, implementing the backend contract plugin.Plugin
//     (Name/Models/Mount) plus any optional interfaces (MenuProvider here), and
//   - the JS package in ./web, implementing the frontend UIPlugin contract
//     against @octarq/plugin-sdk.
//
// See https://github.com/octarq-org/octarq/blob/main/docs/PLUGINS.md for the
// full author guide (background jobs, inter-plugin services, MCP tools, …).
package myplugin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
)

// Plugin is the exported unit a host wires up with app.Use(myplugin.Plugin{}).
type Plugin struct{}

// Name is the stable identifier. It MUST match the frontend UIPlugin's `name`
// (see web/index.ts) so the two halves are traceable to each other.
func (Plugin) Name() string { return "myplugin" }

// Models returns the GORM models this plugin owns; they are migrated for you,
// together with the core models, before any route is served. Stateless here.
func (Plugin) Models() []any { return nil }

// Mount registers the plugin's HTTP routes on the shared API mux. Every route
// is auto-gated by the host: if this feature is disabled for the caller's
// workspace, the app answers 404 before the handler runs — which is exactly the
// state the frontend page renders its neutral "not in this build" fallback for.
// Wrap routes that need a session with ctx.Guard.
func (Plugin) Mount(mux plugin.Mux, ctx *plugin.Context) {
	mux.Handle("GET /api/myplugin/ping", ctx.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "hello from the plugin template",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})))
}

// Menus contributes a sidebar link. Category names the sidebar GROUP the entry
// joins; by convention it equals the group's label ("Workspace" holds
// Overview). A category with no matching group creates one. Keep it in sync
// with web/index.ts.
func (Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{ID: "myplugin", Label: "My Plugin", Path: "/myplugin", Icon: "🧩", Category: "Workspace"},
	}
}

// Compile-time assertions that Plugin satisfies the contracts it claims. Add
// one line per optional interface you implement (Starter, MCPProvider,
// Describer, …) — the host detects optional capabilities by runtime type
// assertion, so a typo'd method name silently never runs WITHOUT these asserts.
var (
	_ plugin.Plugin       = Plugin{}
	_ plugin.MenuProvider = Plugin{}
)
