package pt

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/octarq-org/octarq/server/plugin"
)

// Permission keys. Read, sign-in, and refresh are member work: a team member
// runs the account day to day. Anything that changes configuration or
// credentials is admin work, because it can send requests to a third-party site
// on the workspace's behalf.
const (
	permRead    = "pt.trackers.read"
	permCreate  = "pt.trackers.create"
	permUpdate  = "pt.trackers.update"
	permDelete  = "pt.trackers.delete"
	permSignin  = "pt.trackers.signin"
	permRefresh = "pt.trackers.refresh"
)

// roleMember and roleAdmin are the minimum roles passed to RequirePerm. The
// resolver may still grant or deny by policy.
const (
	roleMember = "member"
	roleAdmin  = "admin"
)

func declarePerms() {
	plugin.DeclarePerm(
		plugin.Perm{Key: permRead, Module: "pt", Resource: "PT Tracker", Action: "read", Label: "View tracker statistics", Default: roleMember},
		plugin.Perm{Key: permCreate, Module: "pt", Resource: "PT Tracker", Action: "create", Label: "Add a tracker", Default: roleAdmin},
		plugin.Perm{Key: permUpdate, Module: "pt", Resource: "PT Tracker", Action: "update", Label: "Change tracker settings and credentials", Default: roleAdmin},
		plugin.Perm{Key: permDelete, Module: "pt", Resource: "PT Tracker", Action: "delete", Label: "Delete a tracker", Default: roleAdmin},
		plugin.Perm{Key: permSignin, Module: "pt", Resource: "PT Tracker", Action: "update", Label: "Sign in to a tracker", Default: roleMember},
		plugin.Perm{Key: permRefresh, Module: "pt", Resource: "PT Tracker", Action: "update", Label: "Refresh tracker statistics", Default: roleMember},
	)
}

// registerRoutes wires the HTTP surface. Mount returns before this runs when
// the host has no guard, so every route below requires an authenticated
// dashboard session.
func (p *Plugin) registerRoutes(ctx *plugin.Context) {
	declarePerms()

	api := ctx.Huma
	if api == nil {
		return
	}

	huma.Register(api, huma.Operation{
		Method: "GET", Path: "/api/pt/overview", Summary: "PT overview", Tags: []string{"PT"},
	}, p.getOverview)
	huma.Register(api, huma.Operation{
		Method: "GET", Path: "/api/pt/trackers", Summary: "List PT trackers", Tags: []string{"PT"},
	}, p.listTrackersHandler)
	huma.Register(api, huma.Operation{
		Method: "POST", Path: "/api/pt/trackers", Summary: "Add a PT tracker", Tags: []string{"PT"}, DefaultStatus: 201,
	}, p.createTrackerHandler)
	huma.Register(api, huma.Operation{
		Method: "POST", Path: "/api/pt/detect", Summary: "Detect a PT site type", Tags: []string{"PT"},
	}, p.detectHandler)
	huma.Register(api, huma.Operation{
		Method: "GET", Path: "/api/pt/trackers/{id}", Summary: "Get a PT tracker", Tags: []string{"PT"},
	}, p.getTrackerHandler)
	huma.Register(api, huma.Operation{
		Method: "PUT", Path: "/api/pt/trackers/{id}", Summary: "Update a PT tracker", Tags: []string{"PT"},
	}, p.updateTrackerHandler)
	huma.Register(api, huma.Operation{
		Method: "DELETE", Path: "/api/pt/trackers/{id}", Summary: "Delete a PT tracker", Tags: []string{"PT"},
	}, p.deleteTrackerHandler)
	huma.Register(api, huma.Operation{
		Method: "PUT", Path: "/api/pt/trackers/{id}/credentials", Summary: "Replace tracker credentials", Tags: []string{"PT"},
	}, p.setCredentialHandler)
	huma.Register(api, huma.Operation{
		Method: "POST", Path: "/api/pt/trackers/{id}/login", Summary: "Sign in to the site", Tags: []string{"PT"},
	}, p.loginHandler)
	huma.Register(api, huma.Operation{
		Method: "POST", Path: "/api/pt/trackers/{id}/signin", Summary: "Run the daily sign-in", Tags: []string{"PT"},
	}, p.signinHandler)
	huma.Register(api, huma.Operation{
		Method: "POST", Path: "/api/pt/trackers/{id}/refresh", Summary: "Refresh tracker statistics", Tags: []string{"PT"},
	}, p.refreshHandler)
	huma.Register(api, huma.Operation{
		Method: "GET", Path: "/api/pt/trackers/{id}/snapshots", Summary: "Tracker statistics history", Tags: []string{"PT"},
	}, p.snapshotsHandler)
	huma.Register(api, huma.Operation{
		Method: "GET", Path: "/api/pt/trackers/{id}/signins", Summary: "Tracker sign-in history", Tags: []string{"PT"},
	}, p.signinsHandler)
}
