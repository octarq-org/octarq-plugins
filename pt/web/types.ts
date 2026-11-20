
export type Outcome =
  | "success"
  | "already_signed"
  | "login_ok_unconfirmed"
  | "needs_manual_action"
  | "auth_failed"
  | "rate_limited"
  | "network_error"
  | "parse_error"
  | "unsupported";

export interface Capabilities {
  login: boolean;
  cookie_auth: boolean;
  api_key_auth: boolean;
  signin: boolean;
  signin_note?: string;
  stats: boolean;
  bonus: boolean;
  warnings: boolean;
  hit_and_run: boolean;
  invites: boolean;
  captcha: boolean;
}

export interface SchemaInfo {
  schema: string;
  display_name: string;
  capabilities: Capabilities;
  defaults: {
    TimeoutSeconds: number;
    LimitIntervalSeconds: number;
    LimitCount: number;
    LimitSeconds: number;
    UserAgent: string;
  };
  domains?: string[];
}

export interface CredentialView {
  has_login_secret: boolean;
  has_cookie: boolean;
  has_api_key: boolean;
  secret_version: number;
  cookie_set_at?: string;
  last_login_at?: string;
}

export interface Snapshot {
  id: number;
  tracker_id: number;
  captured_at: string;
  source: string;
  user_id?: string;
  username?: string;
  user_level?: string;
  join_at?: string;
  uploaded_bytes?: number;
  downloaded_bytes?: number;
  ratio?: number;
  ratio_infinite: boolean;
  bonus?: number;
  seeding?: number;
  leeching?: number;
  seeding_size_bytes?: number;
  leeching_size_bytes?: number;
  unread_messages?: number;
  warning_count?: number;
  hit_and_run_count?: number;
  invites?: number;
  suspended?: boolean;
  warned_until?: string;
  extras?: string;
}

export interface SigninResult {
  id: number;
  tracker_id: number;
  attempted_at: string;
  trigger: string;
  outcome: Outcome;
  reason?: string;
  message?: string;
  evidence?: string;
  bonus_awarded?: number;
  http_status?: number;
  duration_ms?: number;
}

export interface Tracker {
  id: number;
  created_at: string;
  updated_at: string;
  name: string;
  base_url: string;
  api_base_url?: string;
  domain: string;
  schema: string;
  username?: string;
  user_agent?: string;
  timezone?: string;
  enabled: boolean;
  signin_enabled: boolean;
  signin_window_start_min: number;
  signin_window_end_min: number;
  stats_interval_hours: number;
  timeout_seconds: number;
  limit_interval_seconds: number;
  limit_count: number;
  limit_seconds: number;
  cookie_valid: boolean;
  cookie_refreshed_at?: string;
  last_login_at?: string;
  last_signin_at?: string;
  last_signin_outcome?: Outcome;
  last_stats_at?: string;
  next_signin_at?: string;
  next_stats_at?: string;
  needs_manual_action: boolean;
  manual_reason?: string;
  last_error?: string;
  last_error_at?: string;
  consecutive_failures: number;
  created_by_user_id?: number;
  schema_label: string;
  capabilities: Capabilities;
  credential: CredentialView;
  latest_snapshot?: Snapshot;
  last_signin?: SigninResult;
}

export interface Summary {
  tracker_count: number;
  enabled_count: number;
  cookie_invalid_count: number;
  manual_action_count: number;
  uploaded_bytes?: number;
  downloaded_bytes?: number;
  ratio?: number;
  ratio_infinite: boolean;
  bonus?: number;
  warning_count: number;
}

export interface Overview {
  summary: Summary;
  schemas: SchemaInfo[];
  scheduler_available: boolean;
  server_timezone: string;
}

export interface ListResponse {
  trackers: Tracker[];
  summary: Summary;
}

export interface ActionOutcome {
  outcome: Outcome;
  reason?: string;
  message?: string;
  cookie_valid: boolean;
  user_id?: string;
  bonus_awarded?: number;
  attempted_at: string;
  next_signin_at?: string;
  next_stats_at?: string;
}

export interface CredentialWriteResult {
  stored: boolean;
  verified: boolean;
  outcome: string;
  reason?: string;
  message?: string;
  cookie_valid: boolean;
}

export interface DetectResult {
  suggested_schema?: string;
  reachable: boolean;
  http_status?: number;
  has_login_form: boolean;
  has_captcha: boolean;
  challenge: boolean;
  message?: string;
}

export interface TrackerWriteBody {
  name?: string;
  schema?: string;
  base_url?: string;
  api_base_url?: string;
  username?: string;
  login_secret?: string;
  cookie?: string;
  site_api_token?: string;
  user_agent?: string;
  timezone?: string;
  enabled?: boolean;
  signin_enabled?: boolean;
  signin_window_start_min?: number;
  signin_window_end_min?: number;
  stats_interval_hours?: number;
  timeout_seconds?: number;
  limit_interval_seconds?: number;
  limit_count?: number;
  limit_seconds?: number;
}
