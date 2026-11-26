package myplugin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
)

type Plugin struct{}

func (Plugin) Name() string { return "myplugin" }

func (Plugin) Models() []any { return nil }

func (Plugin) Mount(mux plugin.Mux, ctx *plugin.Context) {
	mux.Handle("GET /api/myplugin/ping", ctx.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "hello from the plugin template",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})))
}

func (Plugin) Menus() []plugin.MenuItem {
	return []plugin.MenuItem{
		{ID: "myplugin", Label: "My Plugin", Path: "/myplugin", Icon: "🧩", Category: "Workspace", Area: "operations"},
	}
}

var (
	_ plugin.Plugin       = Plugin{}
	_ plugin.MenuProvider = Plugin{}
)
