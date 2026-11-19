package pt

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/octarq-org/octarq/server/plugin"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

// errNoPersonalSpace refuses a tool call from an account with no personal org.
// The active workspace is never a substitute.
var errNoPersonalSpace = plugin.NewAgentError(
	403,
	"FORBIDDEN_NO_PERSONAL_SPACE",
	"this account has no personal space",
	"Sign in to the dashboard once so the personal space is provisioned.",
	false,
)

// errNoActor refuses a tool call that carries no user. Personal data is only
// readable by the account that owns it.
var errNoActor = plugin.NewAgentError(
	401,
	"UNAUTHORIZED_NO_ACTOR",
	"no user in this request",
	"Use an API token from a signed-in account.",
	false,
)

// errNoRole refuses a write tool called over a transport that carries no
// identity. The stdio transport stamps a user but no token role.
var errNoRole = plugin.NewAgentError(
	403,
	"FORBIDDEN_NO_ROLE",
	"this transport carries no role",
	"Use an API token that carries a role. The local command line has no role.",
	false,
)

// MCPTier reports the license tier these tools require. The plugin ships in the
// Pro build only, so no per-tool tier applies.
func (*Plugin) MCPTier() string { return "" }

// RegisterMCP publishes the read tools and the two member actions. There is no
// tool that changes configuration or reads credentials.
func (p *Plugin) RegisterMCP(srv *mcp.Server) {
	plugin.AddMCPTool(srv, "pt", &mcp.Tool{
		Name:        "pt_list_trackers",
		Description: "List the private-tracker accounts in your personal space with their latest statistics.",
	}, p.mcpListTrackers)

	plugin.AddMCPTool(srv, "pt", &mcp.Tool{
		Name:        "pt_tracker_status",
		Description: "Read one tracker: session state, latest statistics, next scheduled run, and last sign-in result.",
	}, p.mcpTrackerStatus)

	plugin.AddMCPTool(srv, "pt", &mcp.Tool{
		Name:        "pt_tracker_history",
		Description: "Read the statistics history of one tracker, newest first.",
	}, p.mcpTrackerHistory)

	// The write tools decrypt a stored credential. The stdio composition
	// mounts with a no-op host, so registering them there would expose tools
	// that fail on every call.
	if !p.vaultAvailable() {
		return
	}

	plugin.AddMCPTool(srv, "pt", &mcp.Tool{
		Name:        "pt_signin",
		Description: "Run the daily sign-in of one tracker now. The call is idempotent unless force is set.",
	}, p.mcpSignin)

	plugin.AddMCPTool(srv, "pt", &mcp.Tool{
		Name:        "pt_refresh_stats",
		Description: "Read the current statistics of one tracker now and store a snapshot.",
	}, p.mcpRefreshStats)
}

// vaultAvailable reports whether this mount can resolve VaultService.
func (p *Plugin) vaultAvailable() bool {
	_, err := p.vault()
	return err == nil
}

type mcpTrackerArgs struct {
	ID   uint   `json:"id,omitempty" jsonschema:"Tracker id. Use either id or name."`
	Name string `json:"name,omitempty" jsonschema:"Tracker name. Use either id or name."`
}

type mcpHistoryArgs struct {
	ID    uint `json:"id,omitempty" jsonschema:"Tracker id. Use either id or name."`
	Limit int  `json:"limit,omitempty" jsonschema:"Number of snapshots to return (default 30, max 200)."`
}

type mcpSigninArgs struct {
	ID    uint   `json:"id,omitempty" jsonschema:"Tracker id. Use either id or name."`
	Name  string `json:"name,omitempty" jsonschema:"Tracker name. Use either id or name."`
	Force bool   `json:"force,omitempty" jsonschema:"Sign in even when today's ledger already counts as done."`
}

type mcpTrackerOut struct {
	ID                uint              `json:"id"`
	Name              string            `json:"name"`
	Schema            string            `json:"schema"`
	Enabled           bool              `json:"enabled"`
	SigninEnabled     bool              `json:"signin_enabled"`
	CookieValid       bool              `json:"cookie_valid"`
	NeedsManualAction bool              `json:"needs_manual_action"`
	ManualReason      string            `json:"manual_reason,omitempty"`
	LastError         string            `json:"last_error,omitempty"`
	LastSigninAt      *time.Time        `json:"last_signin_at,omitempty"`
	LastSigninOutcome string            `json:"last_signin_outcome,omitempty"`
	LastStatsAt       *time.Time        `json:"last_stats_at,omitempty"`
	NextSigninAt      *time.Time        `json:"next_signin_at,omitempty"`
	NextStatsAt       *time.Time        `json:"next_stats_at,omitempty"`
	CanSignin         bool              `json:"can_signin"`
	Latest            *mcpStatsOut      `json:"latest,omitempty"`
	Credential        *mcpCredentialOut `json:"credential,omitempty"`
}

type mcpCredentialOut struct {
	HasLoginSecret bool `json:"has_login_secret"`
	HasCookie      bool `json:"has_cookie"`
	HasAPIKey      bool `json:"has_api_key"`
}

type mcpStatsOut struct {
	CapturedAt      time.Time  `json:"captured_at"`
	UserLevel       string     `json:"user_level,omitempty"`
	UploadedBytes   *int64     `json:"uploaded_bytes,omitempty"`
	DownloadedBytes *int64     `json:"downloaded_bytes,omitempty"`
	Ratio           *float64   `json:"ratio,omitempty"`
	RatioInfinite   bool       `json:"ratio_infinite"`
	Bonus           *float64   `json:"bonus,omitempty"`
	Seeding         *int       `json:"seeding,omitempty"`
	Leeching        *int       `json:"leeching,omitempty"`
	WarningCount    *int       `json:"warning_count,omitempty"`
	HitAndRunCount  *int       `json:"hit_and_run_count,omitempty"`
	Invites         *int       `json:"invites,omitempty"`
	Suspended       *bool      `json:"suspended,omitempty"`
	WarnedUntil     *time.Time `json:"warned_until,omitempty"`
}

type mcpListOut struct {
	Trackers []mcpTrackerOut `json:"trackers"`
	Total    int             `json:"total"`
}

type mcpSigninOut struct {
	Outcome      string     `json:"outcome"`
	Reason       string     `json:"reason,omitempty"`
	Message      string     `json:"message,omitempty"`
	AttemptedAt  time.Time  `json:"attempted_at"`
	BonusAwarded *float64   `json:"bonus_awarded,omitempty"`
	NextSigninAt *time.Time `json:"next_signin_at,omitempty"`
}

// personalScope resolves the calling account and its personal org. Both are
// required: a zero user or a zero org refuses the call instead of falling back
// to the active workspace.
func (p *Plugin) personalScope(ctx context.Context) (orgID, uid uint, err error) {
	uid = plugin.UserIDFromContext(ctx)
	if uid == 0 {
		return 0, 0, errNoActor
	}
	orgID = p.pluginContext().PersonalOrgFor(uid)
	if orgID == 0 {
		return 0, 0, errNoPersonalSpace
	}
	return orgID, uid, nil
}

func (p *Plugin) mcpListTrackers(ctx context.Context, _ *mcp.CallToolRequest, _ mcpTrackerArgs) (*mcp.CallToolResult, any, error) {
	orgID, uid, err := p.personalScope(ctx)
	if err != nil {
		return nil, nil, err
	}
	list, err := p.listTrackers(ctx, orgID, uid)
	if err != nil {
		return nil, nil, toolError(err)
	}
	out := mcpListOut{Trackers: make([]mcpTrackerOut, 0, len(list))}
	for i := range list {
		out.Trackers = append(out.Trackers, p.mcpView(ctx, &list[i]))
	}
	out.Total = len(out.Trackers)
	return nil, out, nil
}

func (p *Plugin) mcpTrackerStatus(ctx context.Context, _ *mcp.CallToolRequest, in mcpTrackerArgs) (*mcp.CallToolResult, any, error) {
	orgID, uid, err := p.personalScope(ctx)
	if err != nil {
		return nil, nil, err
	}
	t, err := p.resolveTracker(ctx, orgID, uid, in.ID, in.Name)
	if err != nil {
		return nil, nil, err
	}
	return nil, p.mcpView(ctx, t), nil
}

func (p *Plugin) mcpTrackerHistory(ctx context.Context, _ *mcp.CallToolRequest, in mcpHistoryArgs) (*mcp.CallToolResult, any, error) {
	orgID, uid, err := p.personalScope(ctx)
	if err != nil {
		return nil, nil, err
	}
	t, err := p.resolveTracker(ctx, orgID, uid, in.ID, "")
	if err != nil {
		return nil, nil, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}
	rows, listErr := p.listSnapshots(ctx, orgID, t.ID, limit)
	if listErr != nil {
		return nil, nil, toolError(listErr)
	}
	out := make([]mcpStatsOut, 0, len(rows))
	for i := range rows {
		out = append(out, statsOut(&rows[i]))
	}
	return nil, map[string]any{"tracker_id": t.ID, "tracker": t.Name, "snapshots": out}, nil
}

func (p *Plugin) mcpSignin(ctx context.Context, _ *mcp.CallToolRequest, in mcpSigninArgs) (*mcp.CallToolResult, any, error) {
	if plugin.RoleFromContext(ctx) == "" {
		return nil, nil, errNoRole
	}
	orgID, uid, err := p.personalScope(ctx)
	if err != nil {
		return nil, nil, err
	}
	t, err := p.resolveTracker(ctx, orgID, uid, in.ID, in.Name)
	if err != nil {
		return nil, nil, err
	}
	res, signinErr := p.signin(ctx, t, triggerManual, in.Force)
	if signinErr != nil {
		var deferred *deferredError
		if !errors.As(signinErr, &deferred) {
			return nil, nil, toolError(signinErr)
		}
	}
	if res == nil {
		return nil, nil, plugin.NewAgentError(429, "RATE_LIMITED", "the site needs a pause", "Try again later.", true)
	}
	return nil, mcpSigninOut{
		Outcome:      res.Outcome,
		Reason:       res.Reason,
		Message:      res.Message,
		AttemptedAt:  res.AttemptedAt,
		BonusAwarded: res.BonusAwarded,
		NextSigninAt: t.NextSigninAt,
	}, nil
}

func (p *Plugin) mcpRefreshStats(ctx context.Context, _ *mcp.CallToolRequest, in mcpTrackerArgs) (*mcp.CallToolResult, any, error) {
	if plugin.RoleFromContext(ctx) == "" {
		return nil, nil, errNoRole
	}
	orgID, uid, err := p.personalScope(ctx)
	if err != nil {
		return nil, nil, err
	}
	t, err := p.resolveTracker(ctx, orgID, uid, in.ID, in.Name)
	if err != nil {
		return nil, nil, err
	}
	snap, refreshErr := p.refresh(ctx, t, triggerManual)
	if refreshErr != nil {
		var deferred *deferredError
		if errors.As(refreshErr, &deferred) {
			return nil, nil, plugin.NewAgentError(429, "RATE_LIMITED", "the site needs a pause", "Try again later.", true)
		}
		return nil, nil, toolError(refreshErr)
	}
	return nil, statsOut(snap), nil
}

// resolveTracker loads a tracker by id, or by name when no id is given.
func (p *Plugin) resolveTracker(ctx context.Context, orgID, ownerUserID, id uint, name string) (*PTTracker, error) {
	if id != 0 {
		t, err := p.findTracker(ctx, orgID, ownerUserID, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, plugin.NewAgentError(404, "NOT_FOUND", "tracker not found", "Check the id.", false)
			}
			return nil, toolError(err)
		}
		return t, nil
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, plugin.NewAgentError(422, "INVALID_ARGUMENT", "give an id or a name", "Send either id or name.", false)
	}
	t, err := p.findTrackerByName(ctx, orgID, ownerUserID, trimmed)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, plugin.NewAgentError(404, "NOT_FOUND", "tracker not found", "Check the name.", false)
		}
		return nil, toolError(err)
	}
	return t, nil
}

func (p *Plugin) mcpView(ctx context.Context, t *PTTracker) mcpTrackerOut {
	out := mcpTrackerOut{
		ID:                t.ID,
		Name:              t.Name,
		Schema:            t.SchemaKey,
		Enabled:           t.Enabled,
		SigninEnabled:     t.SigninEnabled,
		CookieValid:       t.CookieValid,
		NeedsManualAction: t.NeedsManualAction,
		ManualReason:      t.ManualReason,
		LastError:         t.LastError,
		LastSigninAt:      t.LastSigninAt,
		LastSigninOutcome: t.LastSigninOutcome,
		LastStatsAt:       t.LastStatsAt,
		NextSigninAt:      t.NextSigninAt,
		NextStatsAt:       t.NextStatsAt,
	}
	if ad, err := p.registry().Get(adapter.Schema(t.SchemaKey)); err == nil {
		out.CanSignin = ad.Capabilities().Signin
	}
	if meta := p.vaultMetaOf(ctx, t); meta != nil {
		cv := credentialViewFromMeta(t, meta)
		out.Credential = &mcpCredentialOut{
			HasLoginSecret: cv.HasLoginSecret,
			HasCookie:      cv.HasCookie,
			HasAPIKey:      cv.HasAPIKey,
		}
	}
	if snap, err := p.latestSnapshot(ctx, t.OrgID, t.ID); err == nil && snap != nil {
		stats := statsOut(snap)
		out.Latest = &stats
	}
	return out
}

func statsOut(snap *PTSnapshot) mcpStatsOut {
	if snap == nil {
		return mcpStatsOut{}
	}
	return mcpStatsOut{
		CapturedAt:      snap.CapturedAt,
		UserLevel:       snap.UserLevel,
		UploadedBytes:   snap.UploadedBytes,
		DownloadedBytes: snap.DownloadedBytes,
		Ratio:           snap.Ratio,
		RatioInfinite:   snap.RatioInfinite,
		Bonus:           snap.Bonus,
		Seeding:         snap.Seeding,
		Leeching:        snap.Leeching,
		WarningCount:    snap.WarningCount,
		HitAndRunCount:  snap.HitAndRunCount,
		Invites:         snap.Invites,
		Suspended:       snap.Suspended,
		WarnedUntil:     snap.WarnedUntil,
	}
}

// toolError turns an internal failure into an agent error without leaking
// internals. The tenant and vault cases keep their distinct codes.
func toolError(err error) error {
	switch {
	case errors.Is(err, errTenantUnavailable), errors.Is(err, errVaultUnavailable):
		return plugin.NewAgentError(503, "UNAVAILABLE", "the server cannot complete the call", "Contact your administrator.", true)
	case errors.Is(err, ErrNotFound):
		return plugin.NewAgentError(404, "NOT_FOUND", "tracker not found", "Check the id or the name.", false)
	case errors.Is(err, errNoCredential):
		return plugin.NewAgentError(422, "NO_CREDENTIAL", "the tracker has no credential", "Add the account secret or a cookie in the dashboard.", false)
	default:
		return plugin.NewAgentError(500, "INTERNAL", "the call failed", "Try again. If it fails again, contact your administrator.", true)
	}
}
