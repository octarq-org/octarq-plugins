# Octarq PT Accounts Plugin — Engineering Specification

| Attribute | Value |
|---|---|
| **Plugin ID** | `pt` |
| **Go Module** | `github.com/octarq-org/octarq-plugins/pt` |
| **UI Package** | `@octarq/plugin-pt` |
| **Tier Classification** | Tier 2 (Module Engineering Spec — strictly NOT embedded in the Go binary) |
| **Version** | `0.1.0` |

---

## 1. Scope

The plugin manages private-tracker (PT) accounts. It performs four jobs:

1. Store an account secret per site, encrypted, in the owner's personal space.
2. Log in again on demand (server-side form post or API key check).
3. Run the daily sign-in inside a per-site time window.
4. Read level, traffic, ratio, bonus, warnings, and seeding numbers.

Out of scope for v1: browsers, captcha solving, Cloudflare bypass, 2FA automation, torrent download, RSS, search, media libraries, and outbound proxies.

---

## 2. Data Model

Three tables. Every tracker belongs to one user and lives in that user's personal org. Each row carries `owner_user_id` and `org_id` directly; the plugin never joins through another tenant table. Models use explicit `ID/CreatedAt/UpdatedAt` and no soft delete: deleting a tracker must remove its Vault secret, and a soft delete would keep it.

### 2.1 `pt_trackers`

| Column | Notes |
|---|---|
| `owner_user_id`, `name` | Unique pair `idx_pt_tracker_owner_name` |
| `org_id` | The owner's personal org; indexed, not unique |
| `secret_id` | Immutable UUID, unique; anchors the Vault `KeyName` (`pt.tracker.<secret_id>`) |
| `vault_item_id` | Foreign ID of the encrypted `tracker_credential` item in `VaultService` |
| `base_url`, `api_base_url`, `domain` | Domain is indexed; API base is separate because M-Team serves the site and the API on different hosts |
| `schema_key` | One of the registered adapters (`nexusphp`, `mteam`, `hddolby`, `rousi`) |
| `username`, `user_agent`, `timezone` | |
| `enabled`, `signin_enabled` | |
| `signin_window_start_min`, `signin_window_end_min` | Minutes of the site-local day; `0/0` means any time |
| `stats_interval_hours`, `timeout_seconds` | |
| `limit_interval_seconds`, `limit_count`, `limit_seconds` | Per-site rate limits |
| `cookie_valid`, `last_login_at`, `last_signin_at`, `last_signin_outcome`, `last_stats_at` | |
| `next_signin_at`, `next_stats_at` | Indexed; the only authority for scheduled work |
| `needs_manual_action`, `manual_reason` | Set when a challenge blocks automation |
| `last_error`, `last_error_at`, `consecutive_failures` | |
| `last_notified_outcome` | Notification de-duplication, not exposed |
| `latest_snapshot_id`, `created_by_user_id` | |

Snapshots and sign-in rows are keyed by the owner's personal org, the same `org_id` as their tracker.

### 2.2 `pt_snapshots`

One row per data refresh. Ordered index `idx_pt_snapshot_org_tracker_time` on `org_id, tracker_id, captured_at`.

Metric columns (`uploaded_bytes`, `downloaded_bytes`, `ratio`, `bonus`, `seeding`, `leeching`, `seeding_size_bytes`, `leeching_size_bytes`, `unread_messages`, `warning_count`, `hit_and_run_count`, `invites`) are pointers. A site that does not report a value stores `NULL`, never `0`. Missing upload or download is a parse error; every other metric is best-effort.

### 2.3 `pt_signin_results`

Append-only ledger. Index `idx_pt_signin_org_tracker_time` on `org_id, tracker_id, attempted_at`, plus an index on `outcome`.

| Column | Notes |
|---|---|
| `trigger` | `cron` or `manual` |
| `outcome` | See §4 |
| `reason`, `message` | Short machine reason and user text |
| `evidence` | Truncated, redacted page text |
| `bonus_awarded`, `http_status`, `duration_ms` | |

---

## 3. Credential Storage (`VaultService`)

Tracker secrets (`login`, `cookie`, `site_api_token`, `token`) are stored in the core `plugin.VaultService` (`item_type="tracker_credential"`, `provider_type="tracker"`, `KeyName="pt.tracker."+tracker.SecretID`), with `vault_item_id` recorded on `pt_trackers`.

- **Personal scope**: every item is a user-scope item (`scope="user"`, `owner_user_id`) in the owner's personal org. A read, update, or delete first proves `meta.Scope == "user"` and `meta.OwnerUserID` matches the tracker's owner. A mismatch reads as not found.
- **Workspace & item isolation**: `VaultService` seals each item under its own per-item `SubjectVault` DEK and enforces `org_id` scoping on every read/write/delete.
- **Write order**: insert the tracker first (it carries `secret_id`), then store the secret in `VaultService` and record `vault_item_id`. A credential write failure deletes the tracker and shreds any created secret.
- **Delete**: a tenant transaction hard-deletes all three tables' rows, then calls `VaultService.DeleteSecret` to shred the DEK.
- **Presence flags without decryption**: `buildView` and `mcpView` call `VaultService.GetMeta` (`FieldKeys`, `CurrentVersion`) and never decrypt secrets for display.
- **No read-back**: no endpoint returns the ciphertext or the plaintext. Responses carry presence flags only.

`Validate` fails closed when `VaultService` is missing: storing account secrets unencrypted is not a degraded mode. The context captured at an HTTP mount is kept across stdio MCP re-mounts (see `Mount`).

---

## 4. Sign-in State Machine

Each attempt ends in exactly one outcome. The order of checks is already → negative → positive → unconfirmed.

| Outcome | Evidence | Scheduler effect |
|---|---|---|
| `success` | Positive marker only (`签到成功`, `本次签到获得…`, API code 0) | Next attempt tomorrow |
| `already_signed` | Already-done marker (`今天已签到`) | Next attempt tomorrow |
| `login_ok_unconfirmed` | Login is valid; the page has neither positive nor negative evidence | Retried later; never upgraded to success |
| `needs_manual_action` | Captcha, Cloudflare challenge, 2FA, or e-mail verification | `needs_manual_action` set, `next_signin_at` cleared |
| `auth_failed` | Negative marker, rejected secret, banned account | Failure counter + 1 |
| `rate_limited` | The site asked for a pause during a manual attempt | Recorded verdict; not a counter increment |
| `network_error` | Transport failure | Failure counter + 1 |
| `parse_error` | The page no longer matches the template | Failure counter + 1 |
| `unsupported` | The adapter declares no sign-in endpoint, or a NexusPHP site has no sign-in link | None |

Hard rules:

- A page with no positive evidence can never become `success`.
- M-Team and HDDolby declare `Signin: false`; their sign-in returns `unsupported` without any HTTP request. They are never queued for sign-in, and `last_signin_at` stays empty.
- Rousi requires `code == 0` for success. Any other code is a failure.
- NexusPHP success markers cover sign-in phrases only. Generic "obtained" text is not evidence.

Successful sign-ins set `cookie_valid`, reset the failure counter, and write `last_signin_at`. Only `success` and `already_signed` write `last_signin_at`; failed and unconfirmed attempts write `last_signin_outcome` and `last_error` alone. A cookie-expired or auth error clears `cookie_valid`; a challenge sets `needs_manual_action`.

---

## 5. Scheduling

| Job | Cron | Purpose |
|---|---|---|
| `pt.tick` | `*/5 * * * *` | The only job that sends outbound requests. It runs due sign-ins and due refreshes in the same tick. |
| `pt.retention` | `0 4 * * *` | Deletes snapshots older than 90 days and sign-in rows older than 180 days, per personal org. |

One tick job keeps all outbound traffic behind one Redis lease. Two separate jobs would take two leases and could hit the same site at the same time.

- `next_signin_at` and `next_stats_at` are the authority. A tick only runs rows whose timestamp has passed.
- A tracker whose adapter declares `Signin: false` is never scheduled for sign-in. The tick and `run-all` skip it, and the card hides the sign-in rows.
- `0/0` is any time. `start < end` is a same-day window. `start > end` crosses midnight.
- Due but outside the window: the next time is pushed to the next window start plus jitter, capped at `min(window length, 30m)`, seeded by tracker ID.
- A site-local day that already ended in `success` or `already_signed` is pushed to tomorrow.
- Tick context budget: 8 minutes; single-site timeout: `timeout_seconds + 10s`; at most 4 sites at once.
- Without `Context.RegisterCron` the plugin still mounts. The API reports `scheduler_available: false` and manual actions keep working.

### 5.1 Rate limiting

- Per-domain mutex plus a minimum gap, held in process.
- The minimum gap also has a database claim: `UPDATE pt_trackers SET last_request_at = ? WHERE id = ? AND org_id = ? AND (last_request_at IS NULL OR last_request_at <= ?)`. `RowsAffected == 0` means another instance just used the domain, so the work is deferred.
- **Known limit**: the count/window limit is per process. A multi-instance deployment enforces the count only inside each process. The database claim covers the minimum gap only.
- A wait longer than 60 seconds is not slept. The tracker is deferred to the next tick and the failure counter does not move. A manual attempt records a `rate_limited` outcome instead.

---

## 6. HTTP API

All routes are registered on the shared huma API under `/api/pt`. Every request resolves the caller's personal org; an account with none gets 403. Another owner's tracker id returns 404, never 403.

| Method | Path | Permission | Minimum role |
|---|---|---|---|
| GET | `/api/pt/overview` | `pt.trackers.read` | member |
| GET | `/api/pt/trackers` | `pt.trackers.read` | member |
| POST | `/api/pt/trackers` | `pt.trackers.create` | member |
| POST | `/api/pt/detect` | `pt.trackers.create` | member |
| POST | `/api/pt/actions/run-all` | `pt.trackers.update` | member |
| GET | `/api/pt/trackers/{id}` | `pt.trackers.read` | member |
| PUT | `/api/pt/trackers/{id}` | `pt.trackers.update` | member |
| DELETE | `/api/pt/trackers/{id}` | `pt.trackers.delete` | member |
| PUT | `/api/pt/trackers/{id}/credentials` | `pt.trackers.update` | member |
| POST | `/api/pt/trackers/{id}/login` | `pt.trackers.update` | member |
| POST | `/api/pt/trackers/{id}/signin` | `pt.trackers.signin` | member |
| POST | `/api/pt/trackers/{id}/refresh` | `pt.trackers.refresh` | member |
| GET | `/api/pt/trackers/{id}/snapshots?limit=90` | `pt.trackers.read` | member |
| GET | `/api/pt/trackers/{id}/signins?limit=30` | `pt.trackers.read` | member |

Every permission defaults to member. Ownership protects the data: the tracker lives in the caller's personal space, so a team role does not grant access to another member's account.

Permission checks go through `Context.HasPerm`. When the host or session is missing, the check returns false, so the request is refused. It is never allowed by default.

A completed action returns HTTP 200 with an outcome, even when the outcome is a failure. Only infrastructure faults map to HTTP errors: tenant or vault unavailable → 503, platform deferral → 429.

The create path verifies a supplied cookie or API key before it writes any row. A rejected secret returns 422 and creates nothing. The credentials endpoint verifies too, unless `force: true`; a forced secret is stored and reported as `stored: false`.

On the index fallback, refusal words such as 禁止 or 无权 do not decide the outcome: a signed-in home page carries rule text that contains them. Only a sign-in response can refuse the request.

`POST /api/pt/actions/run-all` marks every eligible tracker of the caller's personal space due for sign-in or statistics, then starts the `pt.tick` job through `CronService.StartJob`. The reply is 202 with the queued count; a held tick lease returns 409, and a host without a scheduler returns 503. The work itself runs in the normal tick, so rate limits, the per-site timeout, the eight-minute budget, and the ledger stay unchanged.

`POST /api/pt/detect` is advice only. It fetches the home page, reports a suggested schema and any challenge found, and never picks the stored schema. The stored `schema_key` is always the one the user selected.

---

## 7. MCP Tools

| Tool | Kind | Notes |
|---|---|---|
| `pt_list_trackers` | read | Trackers plus summary |
| `pt_tracker_status` | read | Latest snapshot, cookie state, next runs |
| `pt_tracker_history` | read | Snapshot series |
| `pt_signin` | write | Idempotent; the daily ledger applies |
| `pt_refresh_stats` | write | |

The write tools decrypt a stored credential, so they are registered only when the mount has a vault. The stdio MCP composition has none and publishes the three read tools alone.

The personal org always comes from `HostSession.PersonalOrgID` (HTTP) or `Context.PersonalOrgFor` (MCP). Zero is an error; the active team workspace is never a substitute. No tool changes configuration or reads credentials. Write tools refuse a context with no role, because a stdio session has no actor to attribute the request to.

---

## 8. Failure Modes and Guards

| Condition | Behaviour |
|---|---|
| `Host.Crypto()` is nil | `Validate` returns an error; the plugin does not start |
| No personal org for the account | HTTP 403 before any query; never the team workspace |
| Missing or zero owner | Write refused; reads match nothing |
| Missing permission wiring | `HasPerm` returns false |
| Unknown `schema_key` | 422; nothing is written |
| Duplicate name in one personal space | 409 |
| Secret in an update body | 422; use the credentials endpoint |
| Unverifiable secret on create | 422; nothing is written |
| Verification skipped (`force`) | Stored, response says `stored: false` |
| Decryption failure | 503; no partial value is returned |
| Shred failure after delete | Health warning only |
| Scheduling unavailable | `scheduler_available: false`; manual actions still work |
| Response body over 8 MiB | Read is cut off by `io.LimitReader` |
| Loopback or private address | Blocked by `safehttp` for user-supplied URLs |

---

## 9. Non-Functional Notes

- No browser is used. All traffic is plain HTTP with a configurable user agent.
- Default traffic per site: one sign-in per day and one refresh every 6 hours, plus at most one login per session expiry.
- NexusPHP pages are decoded through `x/net/html/charset`, so GBK and Big5 sites work.
- `time/tzdata` is embedded, so time windows work in a container without tzdata.
- Site terms are the operator's responsibility. Automated sign-in breaks the rules of some sites.

---

## 10. Known Limits

- NexusPHP has many forks. Only the common template is guaranteed. A page that no longer matches produces `parse_error` and keeps the previous snapshot; it is never filled with guessed values.
- `login_ok_unconfirmed` looks like a failure to users. The UI and the help docs explain it. The alternative is a false success.
- Count/window rate limiting is per process (§5.1).
- M-Team and HDDolby sign-in is unsupported until the sites publish an endpoint.
- The plugin has no proxy support, because `safehttp` has none.
