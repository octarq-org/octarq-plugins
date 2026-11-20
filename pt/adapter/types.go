package adapter

import (
	"context"
	"time"
)

type Schema string

const (
	SchemaNexusPHP Schema = "nexusphp"
	SchemaMTeam    Schema = "mteam"
	SchemaHDDolby  Schema = "hddolby"
	SchemaRousi    Schema = "rousi"
)

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

type Secret struct {
	Login  string
	Cookie string
	APIKey string
	Token  string
}

type Session struct {
	Site   Site
	Secret Secret
	Client *Client
	Now    time.Time
}

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

type Defaults struct {
	TimeoutSeconds       int
	LimitIntervalSeconds int
	LimitCount           int
	LimitSeconds         int
	UserAgent            string
}

type LoginResult struct {
	LoggedIn     bool
	Cookie       string
	UserID       string
	Username     string
	ManualReason string
}

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

type Outcome string

const (
	OutcomeSuccess            Outcome = "success"
	OutcomeAlreadySigned      Outcome = "already_signed"
	OutcomeLoginOKUnconfirmed Outcome = "login_ok_unconfirmed"
	OutcomeNeedsManualAction  Outcome = "needs_manual_action"
	OutcomeAuthFailed         Outcome = "auth_failed"
	OutcomeRateLimited        Outcome = "rate_limited"
	OutcomeNetworkError       Outcome = "network_error"
	OutcomeParseError         Outcome = "parse_error"
	OutcomeUnsupported        Outcome = "unsupported"
)

type SigninResult struct {
	Outcome      Outcome
	Reason       string
	Message      string
	Evidence     string
	BonusAwarded *float64
	HTTPStatus   int
}

type Adapter interface {
	Schema() Schema
	DisplayName() string
	Capabilities() Capabilities
	Defaults() Defaults

	Login(ctx context.Context, s *Session) (*LoginResult, error)

	CheckSession(ctx context.Context, s *Session) (*LoginResult, error)

	Signin(ctx context.Context, s *Session) (*SigninResult, error)

	Stats(ctx context.Context, s *Session) (*UserStats, error)
}
