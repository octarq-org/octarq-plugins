package pt

import (
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

// credentialView reports what is stored without exposing any part of it. The
// API never returns ciphertext, the account secret, the cookie, or the API key.
type credentialView struct {
	HasLoginSecret bool       `json:"has_login_secret"`
	HasCookie      bool       `json:"has_cookie"`
	HasAPIKey      bool       `json:"has_api_key"`
	SecretVersion  int        `json:"secret_version"`
	CookieSetAt    *time.Time `json:"cookie_set_at,omitempty"`
	LastLoginAt    *time.Time `json:"last_login_at,omitempty"`
}

// trackerView is one tracker plus everything the card needs.
type trackerView struct {
	PTTracker
	SchemaLabel    string               `json:"schema_label"`
	Capabilities   adapter.Capabilities `json:"capabilities"`
	Credential     credentialView       `json:"credential"`
	LatestSnapshot *PTSnapshot          `json:"latest_snapshot,omitempty"`
	LastSignin     *PTSigninResult      `json:"last_signin,omitempty"`
}

// summaryView aggregates the visible trackers. Totals cover only the trackers
// whose latest snapshot reported the value, so the numbers never invent data.
type summaryView struct {
	TrackerCount       int      `json:"tracker_count"`
	EnabledCount       int      `json:"enabled_count"`
	CookieInvalidCount int      `json:"cookie_invalid_count"`
	ManualActionCount  int      `json:"manual_action_count"`
	UploadedBytes      *int64   `json:"uploaded_bytes,omitempty"`
	DownloadedBytes    *int64   `json:"downloaded_bytes,omitempty"`
	Ratio              *float64 `json:"ratio,omitempty"`
	RatioInfinite      bool     `json:"ratio_infinite"`
	Bonus              *float64 `json:"bonus,omitempty"`
	WarningCount       int      `json:"warning_count"`
}

// overviewView is the dashboard-level summary plus the capability matrix the
// add form renders.
type overviewView struct {
	Summary            summaryView    `json:"summary"`
	Schemas            []adapter.Info `json:"schemas"`
	SchedulerAvailable bool           `json:"scheduler_available"`
	ServerTimezone     string         `json:"server_timezone"`
}

// trackerWriteBody is the create and update payload. Every field is optional in
// the schema; the handler enforces which ones the current operation needs, so
// create and update can share one shape.
type trackerWriteBody struct {
	Name       string `json:"name,omitempty" maxLength:"120"`
	Schema     string `json:"schema,omitempty" maxLength:"32"`
	BaseURL    string `json:"base_url,omitempty" maxLength:"512"`
	APIBaseURL string `json:"api_base_url,omitempty" maxLength:"512"`
	Username   string `json:"username,omitempty" maxLength:"255"`

	// Secrets. On update an empty string means "keep the stored value".
	LoginSecret string `json:"login_secret,omitempty" maxLength:"512"`
	Cookie      string `json:"cookie,omitempty" maxLength:"8192"`
	APIKey      string `json:"site_api_token,omitempty" maxLength:"512"`

	UserAgent string `json:"user_agent,omitempty" maxLength:"512"`
	Timezone  string `json:"timezone,omitempty" maxLength:"64"`

	Enabled       *bool `json:"enabled,omitempty"`
	SigninEnabled *bool `json:"signin_enabled,omitempty"`

	SigninWindowStartMin *int `json:"signin_window_start_min,omitempty" minimum:"0" maximum:"1439"`
	SigninWindowEndMin   *int `json:"signin_window_end_min,omitempty" minimum:"0" maximum:"1439"`
	StatsIntervalHours   *int `json:"stats_interval_hours,omitempty" minimum:"1" maximum:"168"`
	TimeoutSeconds       *int `json:"timeout_seconds,omitempty" minimum:"5" maximum:"120"`

	LimitIntervalSeconds *int `json:"limit_interval_seconds,omitempty" minimum:"0" maximum:"3600"`
	LimitCount           *int `json:"limit_count,omitempty" minimum:"0" maximum:"1000"`
	LimitSeconds         *int `json:"limit_seconds,omitempty" minimum:"0" maximum:"86400"`
}

// credentialWriteBody replaces stored secrets. Empty fields keep the stored
// value; force stores a secret that the site has not accepted yet.
type credentialWriteBody struct {
	LoginSecret string `json:"login_secret,omitempty" maxLength:"512"`
	Cookie      string `json:"cookie,omitempty" maxLength:"8192"`
	APIKey      string `json:"site_api_token,omitempty" maxLength:"512"`
	Force       bool   `json:"force,omitempty"`
}

// credentialWriteView reports the verification verdict. The secret is stored
// only when Verified is true, or when the caller asked for force.
type credentialWriteView struct {
	Stored      bool   `json:"stored"`
	Verified    bool   `json:"verified"`
	Outcome     string `json:"outcome"`
	Reason      string `json:"reason,omitempty"`
	Message     string `json:"message,omitempty"`
	CookieValid bool   `json:"cookie_valid"`
}

type signinBody struct {
	// Force skips the once-per-day ledger check.
	Force bool `json:"force,omitempty"`
}

type detectBody struct {
	URL string `json:"url" maxLength:"512" required:"true"`
}

// detectView is advice, not a decision: the stored schema is always the one the
// user picks.
type detectView struct {
	SuggestedSchema string `json:"suggested_schema,omitempty"`
	Reachable       bool   `json:"reachable"`
	HTTPStatus      int    `json:"http_status,omitempty"`
	HasLoginForm    bool   `json:"has_login_form"`
	HasCaptcha      bool   `json:"has_captcha"`
	Challenge       bool   `json:"challenge"`
	Message         string `json:"message,omitempty"`
}

type listTrackersInput struct {
	Ctx huma.Context `hidden:"true"`
}

func (i *listTrackersInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type listTrackersOutput struct {
	Body struct {
		Trackers []trackerView `json:"trackers"`
		Summary  summaryView   `json:"summary"`
	}
}

type overviewInput struct {
	Ctx huma.Context `hidden:"true"`
}

func (i *overviewInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type overviewOutput struct {
	Body overviewView
}

type createTrackerInput struct {
	Ctx  huma.Context `hidden:"true"`
	Body trackerWriteBody
}

func (i *createTrackerInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type updateTrackerInput struct {
	Ctx  huma.Context `hidden:"true"`
	ID   uint         `path:"id"`
	Body trackerWriteBody
}

func (i *updateTrackerInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type trackerInput struct {
	Ctx huma.Context `hidden:"true"`
	ID  uint         `path:"id"`
}

func (i *trackerInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type trackerOutput struct {
	Body trackerView
}

type actionOutput struct {
	Body actionOutcome
}

type deleteOutput struct {
	Body map[string]bool
}

type credentialInput struct {
	Ctx  huma.Context `hidden:"true"`
	ID   uint         `path:"id"`
	Body credentialWriteBody
}

func (i *credentialInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type credentialOutput struct {
	Body credentialWriteView
}

type signinInput struct {
	Ctx  huma.Context `hidden:"true"`
	ID   uint         `path:"id"`
	Body signinBody
}

func (i *signinInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type snapshotsInput struct {
	Ctx   huma.Context `hidden:"true"`
	ID    uint         `path:"id"`
	Limit int          `query:"limit" minimum:"1" maximum:"500"`
}

func (i *snapshotsInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type snapshotsOutput struct {
	Body []PTSnapshot
}

type signinsInput struct {
	Ctx   huma.Context `hidden:"true"`
	ID    uint         `path:"id"`
	Limit int          `query:"limit" minimum:"1" maximum:"365"`
}

func (i *signinsInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type signinsOutput struct {
	Body []PTSigninResult
}

type detectInput struct {
	Ctx  huma.Context `hidden:"true"`
	Body detectBody
}

func (i *detectInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type detectOutput struct {
	Body detectView
}
