package pt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/octarq-org/octarq/server/plugin"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

const (
	triggerCron   = "cron"
	triggerManual = "manual"
)

var errNoCredential = errors.New("pt: tracker has no stored credential")

type errManualAction struct{ Reason string }

func (e *errManualAction) Error() string {
	return "pt: manual action required: " + e.Reason
}

type actionOutcome struct {
	Outcome      string     `json:"outcome"`
	Reason       string     `json:"reason,omitempty"`
	Message      string     `json:"message,omitempty"`
	CookieValid  bool       `json:"cookie_valid"`
	UserID       string     `json:"user_id,omitempty"`
	BonusAwarded *float64   `json:"bonus_awarded,omitempty"`
	AttemptedAt  time.Time  `json:"attempted_at"`
	NextSigninAt *time.Time `json:"next_signin_at,omitempty"`
	NextStatsAt  *time.Time `json:"next_stats_at,omitempty"`
}

func siteOf(t *PTTracker) adapter.Site {
	timeout := time.Duration(t.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return adapter.Site{
		ID:         t.ID,
		OrgID:      t.OrgID,
		Name:       t.Name,
		BaseURL:    t.BaseURL,
		APIBaseURL: t.APIBaseURL,
		Domain:     t.Domain,
		Schema:     adapter.Schema(t.SchemaKey),
		Username:   t.Username,
		UserAgent:  t.UserAgent,
		Timeout:    timeout,
		Timezone:   t.location(),
	}
}

func (p *Plugin) sessionFor(ctx context.Context, t *PTTracker) (*adapter.Session, adapter.Adapter, storedSecret, error) {
	var zero storedSecret

	ad, err := p.registry().Get(adapter.Schema(t.SchemaKey))
	if err != nil {
		return nil, nil, zero, err
	}

	if t.VaultItemID == 0 {
		return nil, ad, zero, errNoCredential
	}
	vs, err := p.vault()
	if err != nil {
		return nil, ad, zero, err
	}
	sec, err := vs.GetSecret(ctx, t.OrgID, t.VaultItemID)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			return nil, ad, zero, errNoCredential
		}
		return nil, ad, zero, err
	}
	if err := verifyUserSecret(&sec.Meta, t.OwnerUserID); err != nil {
		return nil, ad, zero, err
	}
	secret := secretFromPayload(sec.Payload)
	if secret.empty() {
		return nil, ad, zero, errNoCredential
	}

	site := siteOf(t)
	client, err := adapter.NewClient(p.client(site.Timeout), site, secret.Cookie)
	if err != nil {
		return nil, ad, zero, err
	}

	return &adapter.Session{
		Site: site,
		Secret: adapter.Secret{
			Login:  secret.Login,
			Cookie: secret.Cookie,
			APIKey: secret.APIKey,
			Token:  secret.Token,
		},
		Client: client,
		Now:    p.clock(),
	}, ad, secret, nil
}

func (p *Plugin) ensureSession(ctx context.Context, t *PTTracker, s *adapter.Session, ad adapter.Adapter, secret storedSecret) error {
	res, err := ad.CheckSession(ctx, s)
	switch {
	case err == nil && res != nil && res.LoggedIn:
		return nil
	case err != nil && !errors.Is(err, adapter.ErrAuth) && !errors.Is(err, adapter.ErrCookieGone):
		return err
	}

	if !ad.Capabilities().Login || secret.Login == "" || strings.TrimSpace(t.Username) == "" {
		return adapter.ErrCookieGone
	}

	loginRes, err := ad.Login(ctx, s)
	if err != nil {
		return err
	}
	if loginRes != nil && loginRes.ManualReason != "" {
		return &errManualAction{Reason: loginRes.ManualReason}
	}
	if loginRes == nil || !loginRes.LoggedIn {
		return adapter.ErrAuth
	}

	if loginRes.Cookie != "" && loginRes.Cookie != secret.Cookie {
		secret.Cookie = loginRes.Cookie
		now := p.clock()
		t.LastLoginAt = &now
		if saveErr := p.storeVaultSecret(ctx, t, secret); saveErr != nil {
			return saveErr
		}
	}
	return nil
}

func (p *Plugin) login(ctx context.Context, t *PTTracker) (*actionOutcome, error) {
	s, ad, secret, err := p.sessionFor(ctx, t)
	if err != nil {
		return nil, err
	}
	if !ad.Capabilities().Login || secret.Login == "" || strings.TrimSpace(t.Username) == "" {
		return &actionOutcome{
			Outcome:     string(adapter.OutcomeUnsupported),
			Reason:      "no_login_credential",
			Message:     "This site needs a pasted cookie. The plugin has no account secret for it.",
			AttemptedAt: p.clock(),
		}, nil
	}

	res, err := ad.Login(ctx, s)
	if err != nil {
		return nil, err
	}
	now := p.clock()
	out := &actionOutcome{AttemptedAt: now}

	switch {
	case res != nil && res.ManualReason != "":
		out.Outcome = string(adapter.OutcomeNeedsManualAction)
		out.Reason = res.ManualReason
		out.Message = manualMessage(res.ManualReason)
		p.markManual(ctx, t, res.ManualReason, now)
	case res != nil && res.LoggedIn:
		cookie := res.Cookie
		if cookie == "" {
			cookie = s.Client.Cookies()
		}
		secret.Cookie = cookie
		t.LastLoginAt = &now
		if saveErr := p.storeVaultSecret(ctx, t, secret); saveErr != nil {
			return nil, saveErr
		}
		out.Outcome = string(adapter.OutcomeSuccess)
		out.Message = "The site accepted the login."
		out.CookieValid = true
		out.UserID = res.UserID
		p.markSessionOK(ctx, t, now)
	default:
		out.Outcome = string(adapter.OutcomeAuthFailed)
		out.Reason = "rejected"
		out.Message = "The site rejected the account. Check the account name and the account secret."
		p.markCookieInvalid(ctx, t, out.Message, now)
	}
	return out, nil
}

func (p *Plugin) refresh(ctx context.Context, t *PTTracker, trigger string) (*PTSnapshot, error) {
	s, ad, secret, err := p.sessionFor(ctx, t)
	if err != nil {
		return nil, err
	}
	now := p.clock()

	if err := p.ensureSession(ctx, t, s, ad, secret); err != nil {
		p.recordSessionError(ctx, t, err, now)
		return nil, err
	}

	if err := p.gateRequest(ctx, t, now); err != nil {
		return nil, err
	}

	stats, err := ad.Stats(ctx, s)
	if err != nil {
		p.recordSessionError(ctx, t, err, now)
		return nil, err
	}

	snap := &PTSnapshot{
		OrgID:             t.OrgID,
		TrackerID:         t.ID,
		CapturedAt:        now,
		Source:            trigger,
		UserID:            stats.UserID,
		Username:          firstNonEmpty(stats.Username, t.Username),
		UserLevel:         stats.UserLevel,
		JoinAt:            stats.JoinAt,
		UploadedBytes:     stats.UploadedBytes,
		DownloadedBytes:   stats.DownloadedBytes,
		Ratio:             stats.Ratio,
		RatioInfinite:     stats.RatioInfinite,
		Bonus:             stats.Bonus,
		Seeding:           stats.Seeding,
		Leeching:          stats.Leeching,
		SeedingSizeBytes:  stats.SeedingSizeBytes,
		LeechingSizeBytes: stats.LeechingSizeBytes,
		UnreadMessages:    stats.UnreadMessages,
		WarningCount:      stats.WarningCount,
		HitAndRunCount:    stats.HitAndRunCount,
		Invites:           stats.Invites,
		Suspended:         stats.Suspended,
		WarnedUntil:       stats.WarnedUntil,
		Extras:            encodeExtras(stats.Extras),
	}
	if err := p.addSnapshot(ctx, snap); err != nil {
		return nil, err
	}

	next := planStatsAt(t, now)
	p.patchTracker(ctx, t, map[string]any{
		"last_stats_at":        now,
		"next_stats_at":        next,
		"latest_snapshot_id":   snap.ID,
		"cookie_valid":         true,
		"consecutive_failures": 0,
		"last_error":           "",
		"last_error_at":        nil,
		"needs_manual_action":  false,
		"manual_reason":        "",
		"last_signin_outcome":  t.LastSigninOutcome,
	})
	t.LastStatsAt = &now
	t.NextStatsAt = &next
	t.LatestSnapshotID = &snap.ID
	return snap, nil
}

func (p *Plugin) signin(ctx context.Context, t *PTTracker, trigger string, force bool) (*PTSigninResult, error) {
	now := p.clock()

	if !force {
		if last, err := p.latestSignin(ctx, t.OrgID, t.ID); err == nil && last != nil {
			if sameDay(t, last.AttemptedAt, now) && signinCountsAsDone(last.Outcome) {
				return last, nil
			}
		}
	}

	ad, adErr := p.registry().Get(adapter.Schema(t.SchemaKey))
	if adErr != nil {
		return p.recordSigninFailure(ctx, t, trigger, adErr, now), nil
	}
	if !ad.Capabilities().Signin {
		res := &PTSigninResult{
			OrgID: t.OrgID, TrackerID: t.ID, AttemptedAt: now, Trigger: trigger,
			Outcome: string(adapter.OutcomeUnsupported),
			Reason:  "signin_unsupported",
			Message: "This site type does not offer a public sign-in endpoint.",
		}
		_ = p.addSignin(ctx, res)
		p.patchTracker(ctx, t, map[string]any{
			"last_signin_outcome": res.Outcome,
			"next_signin_at":      planSigninAt(t, now),
		})
		return res, nil
	}

	s, ad, secret, err := p.sessionFor(ctx, t)
	if err != nil {
		return p.recordSigninFailure(ctx, t, trigger, err, now), nil
	}

	if err := p.ensureSession(ctx, t, s, ad, secret); err != nil {
		return p.recordSigninFailure(ctx, t, trigger, err, now), nil
	}

	if err := p.gateRequest(ctx, t, now); err != nil {
		var deferred *deferredError
		if errors.As(err, &deferred) {
			if trigger == triggerCron {
				next := now.Add(cronTickEvery)
				p.patchTracker(ctx, t, map[string]any{"next_signin_at": next})
				return nil, err
			}
			res := &PTSigninResult{
				OrgID: t.OrgID, TrackerID: t.ID, AttemptedAt: now, Trigger: trigger,
				Outcome: string(adapter.OutcomeRateLimited),
				Reason:  "rate_limited",
				Message: "The site needs a pause before the next request. Try again later.",
			}
			if addErr := p.addSignin(ctx, res); addErr != nil {
				return nil, addErr
			}
			p.applySigninOutcome(ctx, t, res, now)
			return res, nil
		}
		return p.recordSigninFailure(ctx, t, trigger, err, now), nil
	}

	started := p.clock()
	res, err := ad.Signin(ctx, s)
	elapsed := p.clock().Sub(started)
	if err != nil {
		res = &adapter.SigninResult{
			Outcome: adapter.Outcome(outcomeFromError(err)),
			Reason:  reasonFromError(err),
			Message: messageFromError(err),
		}
	}

	record := &PTSigninResult{
		OrgID:        t.OrgID,
		TrackerID:    t.ID,
		AttemptedAt:  now,
		Trigger:      trigger,
		Outcome:      string(res.Outcome),
		Reason:       res.Reason,
		Message:      res.Message,
		Evidence:     res.Evidence,
		BonusAwarded: res.BonusAwarded,
		HTTPStatus:   res.HTTPStatus,
		DurationMS:   elapsed.Milliseconds(),
	}
	if err := p.addSignin(ctx, record); err != nil {
		return nil, err
	}

	p.applySigninOutcome(ctx, t, record, now)
	return record, nil
}

func (p *Plugin) applySigninOutcome(ctx context.Context, t *PTTracker, r *PTSigninResult, now time.Time) {
	updates := map[string]any{
		"last_signin_outcome":  r.Outcome,
		"last_error":           "",
		"last_error_at":        nil,
		"needs_manual_action":  false,
		"manual_reason":        "",
		"consecutive_failures": 0,
	}

	switch adapter.Outcome(r.Outcome) {
	case adapter.OutcomeSuccess, adapter.OutcomeAlreadySigned:
		next := planSigninAt(t, now.Add(24*time.Hour))
		updates["last_signin_at"] = now
		updates["next_signin_at"] = next
		updates["cookie_valid"] = true
		t.LastSigninAt = &now
		t.NextSigninAt = &next
		p.notify(ctx, t, r.Outcome, r.Message, now)
	case adapter.OutcomeNeedsManualAction:
		updates["needs_manual_action"] = true
		updates["manual_reason"] = r.Reason
		updates["next_signin_at"] = nil
		updates["consecutive_failures"] = t.ConsecutiveFailures + 1
		updates["last_error"] = r.Message
		updates["last_error_at"] = now
		t.NextSigninAt = nil
		p.notify(ctx, t, r.Outcome, r.Message, now)
	case adapter.OutcomeAuthFailed:
		updates["cookie_valid"] = false
		updates["consecutive_failures"] = t.ConsecutiveFailures + 1
		updates["last_error"] = firstNonEmpty(r.Message, "The site rejected the session.")
		updates["last_error_at"] = now
		next := now.Add(retryDelay(t.ConsecutiveFailures+1, r.Outcome))
		updates["next_signin_at"] = next
		t.NextSigninAt = &next
		p.notify(ctx, t, r.Outcome, r.Message, now)
	default:
		failures := t.ConsecutiveFailures + 1
		updates["consecutive_failures"] = failures
		updates["last_error"] = firstNonEmpty(r.Message, "The sign-in did not complete.")
		updates["last_error_at"] = now
		next := now.Add(retryDelay(failures, r.Outcome))
		updates["next_signin_at"] = next
		t.NextSigninAt = &next
		if failures >= 3 {
			p.notify(ctx, t, r.Outcome, r.Message, now)
		}
	}

	p.patchTracker(ctx, t, updates)
}

func (p *Plugin) recordSigninFailure(ctx context.Context, t *PTTracker, trigger string, err error, now time.Time) *PTSigninResult {
	res := &PTSigninResult{
		OrgID:       t.OrgID,
		TrackerID:   t.ID,
		AttemptedAt: now,
		Trigger:     trigger,
		Outcome:     outcomeFromError(err),
		Reason:      reasonFromError(err),
		Message:     messageFromError(err),
	}
	if addErr := p.addSignin(ctx, res); addErr != nil {
		return res
	}
	p.applySigninOutcome(ctx, t, res, now)
	return res
}

func (p *Plugin) gateRequest(ctx context.Context, t *PTTracker, now time.Time) error {
	_, ad, _, err := p.sessionFor(ctx, t)
	if err != nil {
		return err
	}
	cfg := rateConfigFor(t, ad.Defaults())

	if err := p.limiterRef().acquire(ctx, t.Domain, cfg, now); err != nil {
		return err
	}
	if cfg.Interval <= 0 {
		return nil
	}
	earliest := now.Add(-cfg.Interval)
	claimed, err := p.claimRequestSlot(ctx, t.OrgID, t.ID, now, earliest)
	if err != nil {
		return err
	}
	if !claimed {
		return &deferredError{RetryAfter: cfg.Interval}
	}
	return nil
}

func (p *Plugin) recordSessionError(ctx context.Context, t *PTTracker, err error, now time.Time) {
	updates := map[string]any{
		"consecutive_failures": t.ConsecutiveFailures + 1,
		"last_error":           messageFromError(err),
		"last_error_at":        now,
	}
	if adapter.KindOf(err) == adapter.KindCookieExpired || errors.Is(err, adapter.ErrAuth) {
		updates["cookie_valid"] = false
	}
	if errors.Is(err, adapter.ErrCloudflare) || errors.Is(err, adapter.ErrCaptcha) {
		updates["needs_manual_action"] = true
		updates["manual_reason"] = firstNonEmpty(adapter.ManualReasonOf(err), "manual")
	}
	p.patchTracker(ctx, t, updates)
}

func (p *Plugin) markSessionOK(ctx context.Context, t *PTTracker, now time.Time) {
	updates := map[string]any{
		"cookie_valid":         true,
		"last_login_at":        now,
		"consecutive_failures": 0,
		"last_error":           "",
		"last_error_at":        nil,
		"needs_manual_action":  false,
		"manual_reason":        "",
	}
	t.CookieValid = true
	p.patchTracker(ctx, t, updates)
}

func (p *Plugin) markCookieInvalid(ctx context.Context, t *PTTracker, reason string, now time.Time) {
	p.patchTracker(ctx, t, map[string]any{
		"cookie_valid":         false,
		"last_error":           reason,
		"last_error_at":        now,
		"consecutive_failures": t.ConsecutiveFailures + 1,
	})
}

func (p *Plugin) markManual(ctx context.Context, t *PTTracker, reason string, now time.Time) {
	p.patchTracker(ctx, t, map[string]any{
		"needs_manual_action": true,
		"manual_reason":       reason,
		"next_signin_at":      nil,
		"last_error":          manualMessage(reason),
		"last_error_at":       now,
	})
}

func (p *Plugin) markAllDue(ctx context.Context, orgID, uid uint, kind string) (int64, error) {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return 0, err
	}
	column := "next_signin_at"
	q := tdb.Scoped(&PTTracker{}).
		Where("owner_user_id = ? AND enabled = ? AND needs_manual_action = ?", uid, true, false)
	if kind == "signin" {
		q = q.Where("signin_enabled = ?", true)
		if schemas := p.unsupportedSigninSchemas(); len(schemas) > 0 {
			q = q.Where("schema_key NOT IN ?", schemas)
		}
	} else {
		column = "next_stats_at"
	}
	res := q.Updates(map[string]any{column: p.clock()})
	return res.RowsAffected, res.Error
}

func (p *Plugin) unsupportedSigninSchemas() []string {
	var out []string
	for _, info := range p.registry().List() {
		if !info.Capabilities.Signin {
			out = append(out, string(info.Schema))
		}
	}
	return out
}

func (p *Plugin) patchTracker(ctx context.Context, t *PTTracker, updates map[string]any) {
	tdb, err := p.tenantDB(ctx, t.OrgID)
	if err != nil {
		return
	}
	if err := tdb.Scoped(&PTTracker{}).Where("id = ?", t.ID).Updates(updates).Error; err != nil {
		return
	}
	if v, ok := updates["last_signin_at"].(time.Time); ok {
		t.LastSigninAt = &v
	}
	if v, ok := updates["last_stats_at"].(time.Time); ok {
		t.LastStatsAt = &v
	}
}

func (p *Plugin) notify(ctx context.Context, t *PTTracker, outcome, message string, now time.Time) {
	if p.emit == nil || outcome == "" {
		return
	}
	if t.LastNotifiedOutcome == outcome && t.LastNotifiedAt != nil && now.Sub(*t.LastNotifiedAt) < 24*time.Hour {
		return
	}

	payload := plugin.NotificationPayload{
		EventType: "pt." + outcome,
		Title:     "PT account: " + t.Name,
		Body:      message,
		Priority:  "normal",
		OrgID:     strconv.FormatUint(uint64(t.OrgID), 10),
	}
	if t.CreatedByUserID != 0 {
		payload.UserID = strconv.FormatUint(uint64(t.CreatedByUserID), 10)
	}
	if err := p.emit(ctx, payload); err != nil {
		return
	}

	p.patchTracker(ctx, t, map[string]any{
		"last_notified_at":      now,
		"last_notified_outcome": outcome,
	})
	t.LastNotifiedAt = &now
	t.LastNotifiedOutcome = outcome
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func signinCountsAsDone(outcome string) bool {
	switch adapter.Outcome(outcome) {
	case adapter.OutcomeSuccess, adapter.OutcomeAlreadySigned:
		return true
	}
	return false
}

func encodeExtras(extras map[string]any) string {
	if len(extras) == 0 {
		return ""
	}
	raw, err := json.Marshal(extras)
	if err != nil {
		return ""
	}
	return string(raw)
}

func manualMessage(reason string) string {
	switch reason {
	case "captcha":
		return "The site requires a captcha. Paste a browser cookie to continue."
	case "cloudflare":
		return "The site is behind an anti-bot challenge. Paste a browser cookie to continue."
	case "2fa":
		return "The site asks for a second factor. Paste a browser cookie to continue."
	default:
		return "The site needs a manual step. Paste a browser cookie to continue."
	}
}

func outcomeFromError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, adapter.ErrCloudflare), errors.Is(err, adapter.ErrCaptcha), errors.Is(err, adapter.ErrNeedsManual):
		return string(adapter.OutcomeNeedsManualAction)
	case errors.Is(err, adapter.ErrAuth), errors.Is(err, adapter.ErrCookieGone):
		return string(adapter.OutcomeAuthFailed)
	case errors.Is(err, adapter.ErrRateLimited), errors.Is(err, adapter.ErrUnsupported):
		if errors.Is(err, adapter.ErrUnsupported) {
			return string(adapter.OutcomeUnsupported)
		}
		return string(adapter.OutcomeRateLimited)
	case errors.Is(err, adapter.ErrParse):
		return string(adapter.OutcomeParseError)
	case errors.Is(err, errNoCredential):
		return string(adapter.OutcomeAuthFailed)
	default:
		return string(adapter.OutcomeNetworkError)
	}
}

func reasonFromError(err error) string {
	if reason := adapter.ManualReasonOf(err); reason != "" {
		return reason
	}
	if kind := adapter.KindOf(err); kind != "" {
		return string(kind)
	}
	if errors.Is(err, errNoCredential) {
		return "no_credential"
	}
	return "unknown"
}

func messageFromError(err error) string {
	var manual *errManualAction
	if errors.As(err, &manual) {
		return manualMessage(manual.Reason)
	}
	switch {
	case errors.Is(err, errNoCredential):
		return "The tracker has no stored credential. Add the account secret or paste a cookie."
	case errors.Is(err, adapter.ErrCookieGone):
		return "The saved session expired. Sign in again or paste a browser cookie."
	case errors.Is(err, adapter.ErrAuth):
		return "The site rejected the session. Check the account data."
	case errors.Is(err, adapter.ErrCloudflare):
		return manualMessage("cloudflare")
	case errors.Is(err, adapter.ErrCaptcha):
		return manualMessage("captcha")
	case errors.Is(err, adapter.ErrRateLimited):
		return "The site is rate limiting requests. The plugin will retry later."
	case errors.Is(err, adapter.ErrParse):
		return "The page could not be read. The site layout may have changed."
	case errors.Is(err, adapter.ErrUnsupported):
		return "This site type does not support that action."
	case err != nil:
		return fmt.Sprintf("The request failed: %s", err.Error())
	}
	return ""
}
