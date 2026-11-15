// Package adapter defines the contract every private-tracker site dialect
// implements, plus the shared HTTP and parsing helpers the dialects use.
//
// The package deliberately knows nothing about Octarq: no GORM, no plugin
// context. That keeps each dialect testable against a fixed HTTP fixture and
// keeps the orchestration (credentials, tenancy, scheduling) in the plugin.
package adapter

import (
	"context"
	"time"
)

// Schema identifies a site dialect.
type Schema string

const (
	SchemaNexusPHP Schema = "nexusphp"
	SchemaMTeam    Schema = "mteam"
	SchemaHDDolby  Schema = "hddolby"
	SchemaRousi    Schema = "rousi"
)

// Site is the non-secret configuration of one tracker.
type Site struct {
	ID         uint
	OrgID      uint
	Name       string
	BaseURL    string
	APIBaseURL string
	Domain     string
	Schema     Schema
	Username   string
	UserAgent  string
	Timeout    time.Duration
	Timezone   *time.Location
}

// Secret is the decrypted credential payload. Empty fields mean "not stored".
type Secret struct {
	// Login is the account secret used for form login.
	Login  string
	Cookie string
	APIKey string
	Token  string
}

// Session is everything one adapter call needs.
type Session struct {
	Site   Site
	Secret Secret
	Client *Client
	Now    time.Time
}

// Capabilities tells the UI and the scheduler what this dialect can actually
// do. Advertising a capability the dialect cannot back is how automation turns
// into silent false successes.
type Capabilities struct {
	Login      bool   `json:"login"`
	CookieAuth bool   `json:"cookie_auth"`
	APIKeyAuth bool   `json:"api_key_auth"`
	Signin     bool   `json:"signin"`
	SigninNote string `json:"signin_note,omitempty"`
	Stats      bool   `json:"stats"`
	Bonus      bool   `json:"bonus"`
	Warnings   bool   `json:"warnings"`
	HitAndRun  bool   `json:"hit_and_run"`
	Invites    bool   `json:"invites"`
	Captcha    bool   `json:"captcha"`
}

// Defaults are the per-dialect starting values a new tracker inherits.
type Defaults struct {
	TimeoutSeconds       int
	LimitIntervalSeconds int
	LimitCount           int
	LimitSeconds         int
	UserAgent            string
}

// LoginResult reports the outcome of Login or CheckSession.
//
// LoggedIn is the only field callers may branch on. ManualReason is set when a
// human must act (captcha, Cloudflare, interactive 2FA) and is a stable
// machine key, not prose.
type LoginResult struct {
	LoggedIn     bool
	Cookie       string
	UserID       string
	Username     string
	ManualReason string
}

// UserStats is what a dialect could read. Every metric is a pointer: nil means
// the site did not expose it.
type UserStats struct {
	UserID    string
	Username  string
	UserLevel string
	JoinAt    *time.Time

	UploadedBytes     *int64
	DownloadedBytes   *int64
	Ratio             *float64
	RatioInfinite     bool
	Bonus             *float64
	Seeding           *int
	Leeching          *int
	SeedingSizeBytes  *int64
	LeechingSizeBytes *int64
	UnreadMessages    *int

	WarningCount   *int
	HitAndRunCount *int
	Invites        *int
	Suspended      *bool
	WarnedUntil    *time.Time

	Extras map[string]any
}

// Outcome is the sign-in verdict. It is persisted verbatim and drives the UI
// and the retry policy.
type Outcome string

const (
	// OutcomeSuccess means the site said the sign-in happened.
	OutcomeSuccess Outcome = "success"
	// OutcomeAlreadySigned means the site said the sign-in already happened
	// today. For scheduling this is success; for display it is distinct.
	OutcomeAlreadySigned Outcome = "already_signed"
	// OutcomeLoginOKUnconfirmed means the session worked but the page carried
	// no evidence either way. It must never be reported as success.
	OutcomeLoginOKUnconfirmed Outcome = "login_ok_unconfirmed"
	// OutcomeNeedsManualAction means automation cannot proceed (captcha,
	// Cloudflare, 2FA prompt, verification email).
	OutcomeNeedsManualAction Outcome = "needs_manual_action"
	OutcomeAuthFailed        Outcome = "auth_failed"
	OutcomeRateLimited       Outcome = "rate_limited"
	OutcomeNetworkError      Outcome = "network_error"
	OutcomeParseError        Outcome = "parse_error"
	OutcomeUnsupported       Outcome = "unsupported"
)

// SigninResult is the dialect's verdict plus the evidence that produced it.
type SigninResult struct {
	Outcome      Outcome
	Reason       string
	Message      string
	Evidence     string
	BonusAwarded *float64
	HTTPStatus   int
}

// Adapter is one site dialect.
//
// Signin may legally return an error for transport faults; business verdicts
// belong in SigninResult so the service can persist them without translating
// error strings.
type Adapter interface {
	Schema() Schema
	DisplayName() string
	Capabilities() Capabilities
	Defaults() Defaults

	// Login exchanges stored credentials for a fresh cookie. API-key dialects
	// validate the key through a profile call and return LoggedIn with an
	// empty Cookie.
	Login(ctx context.Context, s *Session) (*LoginResult, error)

	// CheckSession proves whether the stored cookie or API key still works.
	CheckSession(ctx context.Context, s *Session) (*LoginResult, error)

	Signin(ctx context.Context, s *Session) (*SigninResult, error)

	Stats(ctx context.Context, s *Session) (*UserStats, error)
}
