package pt

import "time"

type PTTracker struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	OwnerUserID uint `gorm:"column:owner_user_id;not null;index;uniqueIndex:idx_pt_tracker_owner_name,priority:1" json:"-"`

	OrgID uint `gorm:"column:org_id;not null;index" json:"-"`

	Name string `gorm:"size:120;not null;uniqueIndex:idx_pt_tracker_owner_name,priority:2" json:"name"`

	SecretID    string `gorm:"size:64;not null;uniqueIndex" json:"-"`
	VaultItemID uint   `gorm:"index" json:"-"`

	BaseURL    string `gorm:"size:512;not null" json:"base_url"`
	APIBaseURL string `gorm:"size:512" json:"api_base_url,omitempty"`
	Domain     string `gorm:"size:255;not null;index" json:"domain"`
	SchemaKey  string `gorm:"column:schema_key;size:32;not null;index" json:"schema"`

	Username  string `gorm:"size:255" json:"username,omitempty"`
	UserAgent string `gorm:"size:512" json:"user_agent,omitempty"`
	Timezone  string `gorm:"size:64" json:"timezone,omitempty"`

	Enabled       bool `gorm:"not null;default:true" json:"enabled"`
	SigninEnabled bool `gorm:"not null;default:true" json:"signin_enabled"`

	SigninWindowStartMin int `gorm:"not null;default:0" json:"signin_window_start_min"`
	SigninWindowEndMin   int `gorm:"not null;default:0" json:"signin_window_end_min"`

	StatsIntervalHours int `gorm:"not null;default:6" json:"stats_interval_hours"`
	TimeoutSeconds     int `gorm:"not null;default:20" json:"timeout_seconds"`

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

func (PTTracker) TableName() string { return "pt_trackers" }

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
