package pt

import "time"

// PTTracker is one private-tracker account. A row is workspace-scoped: the
// same tracker can exist in two workspaces with different credentials.
//
// Credentials are not here. They live in PTCredential so that a query that
// lists trackers never loads ciphertext, and so a credential wipe does not
// destroy the tracker's history.
type PTTracker struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	OrgID uint `gorm:"column:org_id;not null;index;uniqueIndex:idx_pt_tracker_org_name,priority:1" json:"-"`

	Name string `gorm:"size:120;not null;uniqueIndex:idx_pt_tracker_org_name,priority:2" json:"name"`

	// SecretID anchors the credential's encryption AAD and subject namespace.
	// It is generated once at creation and never changes, so renaming a
	// tracker or correcting its schema cannot brick the ciphertext.
	SecretID string `gorm:"size:64;not null;uniqueIndex" json:"-"`

	BaseURL string `gorm:"size:512;not null" json:"base_url"`
	// APIBaseURL is separate because the API host is not always derived from
	// the site host (M-Team serves the site at kp.m-team.cc and the API at
	// api.m-team.cc).
	APIBaseURL string `gorm:"size:512" json:"api_base_url,omitempty"`
	Domain     string `gorm:"size:255;not null;index" json:"domain"`
	SchemaKey  string `gorm:"column:schema_key;size:32;not null;index" json:"schema"`

	// Username is the site account name. The matching login secret is stored
	// encrypted in PTCredential, never in this row.
	Username  string `gorm:"size:255" json:"username,omitempty"`
	UserAgent string `gorm:"size:512" json:"user_agent,omitempty"`
	// Timezone is an IANA name. Empty means server local time.
	Timezone string `gorm:"size:64" json:"timezone,omitempty"`

	Enabled       bool `gorm:"not null;default:true" json:"enabled"`
	SigninEnabled bool `gorm:"not null;default:true" json:"signin_enabled"`

	// SigninWindowStartMin/EndMin are minutes from local midnight. 0/0 means
	// any time. End <= Start means the window crosses midnight.
	SigninWindowStartMin int `gorm:"not null;default:0" json:"signin_window_start_min"`
	SigninWindowEndMin   int `gorm:"not null;default:0" json:"signin_window_end_min"`

	StatsIntervalHours int `gorm:"not null;default:6" json:"stats_interval_hours"`
	TimeoutSeconds     int `gorm:"not null;default:20" json:"timeout_seconds"`

	// Rate limiting knobs. Zero means the adapter default applies.
	LimitIntervalSeconds int `gorm:"not null;default:0" json:"limit_interval_seconds"`
	LimitCount           int `gorm:"not null;default:0" json:"limit_count"`
	LimitSeconds         int `gorm:"not null;default:0" json:"limit_seconds"`

	CookieValid       bool       `gorm:"not null;default:false" json:"cookie_valid"`
	CookieRefreshedAt *time.Time `json:"cookie_refreshed_at,omitempty"`
	LastRequestAt     *time.Time `json:"-"`

	LastLoginAt       *time.Time `json:"last_login_at,omitempty"`
	LastSigninAt      *time.Time `json:"last_signin_at,omitempty"`
	LastSigninOutcome string     `gorm:"size:32" json:"last_signin_outcome,omitempty"`
	LastStatsAt       *time.Time `json:"last_stats_at,omitempty"`
	NextSigninAt      *time.Time `gorm:"index" json:"next_signin_at,omitempty"`
	NextStatsAt       *time.Time `gorm:"index" json:"next_stats_at,omitempty"`

	// NeedsManualAction pauses automation until a human supplies a new
	// credential (captcha, Cloudflare, interactive 2FA).
	NeedsManualAction bool   `gorm:"not null;default:false;index" json:"needs_manual_action"`
	ManualReason      string `gorm:"size:64" json:"manual_reason,omitempty"`

	LastError           string     `gorm:"type:text" json:"last_error,omitempty"`
	LastErrorAt         *time.Time `json:"last_error_at,omitempty"`
	ConsecutiveFailures int        `gorm:"not null;default:0" json:"consecutive_failures"`

	LastNotifiedAt      *time.Time `json:"-"`
	LastNotifiedOutcome string     `gorm:"size:32" json:"-"`

	LatestSnapshotID *uint `gorm:"index" json:"latest_snapshot_id,omitempty"`
	CreatedByUserID  uint  `gorm:"index" json:"created_by_user_id,omitempty"`
}

// TableName pins the table name; core migrations are dialect-neutral and the
// pt_ prefix keeps this plugin's tables out of every other module's namespace.
func (PTTracker) TableName() string { return "pt_trackers" }

// PTCredential is the 1:1 encrypted secret store for a tracker.
//
// The plaintext payload is a JSON object of {login_secret, cookie, api_key,
// token}. It is encrypted under a per-workspace subject vault and bound to
// (org_id, SecretID) through the AES-GCM additional data, so a ciphertext
// copied to another workspace or another tracker fails to decrypt.
type PTCredential struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	OrgID     uint `gorm:"column:org_id;not null;index;uniqueIndex:idx_pt_cred_org_tracker,priority:1" json:"-"`
	TrackerID uint `gorm:"not null;uniqueIndex:idx_pt_cred_org_tracker,priority:2" json:"tracker_id"`

	Ciphertext    string `gorm:"type:text;not null" json:"-"`
	SecretVersion int    `gorm:"not null;default:1" json:"secret_version"`

	// Presence flags let the UI and API report what is stored without ever
	// decrypting or revealing it.
	HasLoginSecret bool `gorm:"not null;default:false" json:"has_login_secret"`
	HasCookie      bool `gorm:"not null;default:false" json:"has_cookie"`
	HasAPIKey      bool `gorm:"not null;default:false" json:"has_api_key"`

	CookieSetAt   *time.Time `json:"cookie_set_at,omitempty"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`
	LoginFailures int        `gorm:"not null;default:0" json:"login_failures"`
}

func (PTCredential) TableName() string { return "pt_credentials" }

// PTSnapshot is one immutable statistics capture.
//
// Every metric is a pointer on purpose: a nil means "the site did not expose
// this", which is not the same fact as zero. Zeroing unknowns would make a
// suspended tracker look healthy.
type PTSnapshot struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	OrgID      uint      `gorm:"column:org_id;not null;index:idx_pt_snapshot_org_tracker_time,priority:1" json:"-"`
	TrackerID  uint      `gorm:"not null;index:idx_pt_snapshot_org_tracker_time,priority:2" json:"tracker_id"`
	CapturedAt time.Time `gorm:"not null;index:idx_pt_snapshot_org_tracker_time,priority:3" json:"captured_at"`
	Source     string    `gorm:"size:16;not null" json:"source"`

	UserID    string     `gorm:"size:64" json:"user_id,omitempty"`
	Username  string     `gorm:"size:255" json:"username,omitempty"`
	UserLevel string     `gorm:"size:128" json:"user_level,omitempty"`
	JoinAt    *time.Time `json:"join_at,omitempty"`

	UploadedBytes     *int64   `json:"uploaded_bytes,omitempty"`
	DownloadedBytes   *int64   `json:"downloaded_bytes,omitempty"`
	Ratio             *float64 `json:"ratio,omitempty"`
	RatioInfinite     bool     `gorm:"not null;default:false" json:"ratio_infinite"`
	Bonus             *float64 `json:"bonus,omitempty"`
	Seeding           *int     `json:"seeding,omitempty"`
	Leeching          *int     `json:"leeching,omitempty"`
	SeedingSizeBytes  *int64   `json:"seeding_size_bytes,omitempty"`
	LeechingSizeBytes *int64   `json:"leeching_size_bytes,omitempty"`
	UnreadMessages    *int     `json:"unread_messages,omitempty"`

	WarningCount   *int       `json:"warning_count,omitempty"`
	HitAndRunCount *int       `json:"hit_and_run_count,omitempty"`
	Invites        *int       `json:"invites,omitempty"`
	Suspended      *bool      `json:"suspended,omitempty"`
	WarnedUntil    *time.Time `json:"warned_until,omitempty"`

	Extras string `gorm:"type:text" json:"extras,omitempty"`
}

func (PTSnapshot) TableName() string { return "pt_snapshots" }

// PTSigninResult is one sign-in attempt and its typed outcome.
//
// The table is a ledger, not a flag: "already signed" and "sign-in succeeded"
// are different facts, and "logged in but the site never confirmed the
// sign-in" is a third one that must never be reported as success.
type PTSigninResult struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	OrgID       uint      `gorm:"column:org_id;not null;index:idx_pt_signin_org_tracker_time,priority:1" json:"-"`
	TrackerID   uint      `gorm:"not null;index:idx_pt_signin_org_tracker_time,priority:2" json:"tracker_id"`
	AttemptedAt time.Time `gorm:"not null;index:idx_pt_signin_org_tracker_time,priority:3" json:"attempted_at"`

	Trigger  string `gorm:"size:16;not null" json:"trigger"`
	Outcome  string `gorm:"size:32;not null;index" json:"outcome"`
	Reason   string `gorm:"size:64" json:"reason,omitempty"`
	Message  string `gorm:"type:text" json:"message,omitempty"`
	Evidence string `gorm:"size:255" json:"evidence,omitempty"`

	BonusAwarded *float64 `json:"bonus_awarded,omitempty"`
	HTTPStatus   int      `json:"http_status,omitempty"`
	DurationMS   int64    `json:"duration_ms,omitempty"`
}

func (PTSigninResult) TableName() string { return "pt_signin_results" }
