package pt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq/server/plugin"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

const (
	// cookieMaxBytes bounds a pasted Cookie header. Real sessions are well under
	// 2 KiB; anything larger is a paste accident or an attack on the parser.
	cookieMaxBytes = 8192

	defaultStatsIntervalHours = 6
	defaultTimeoutSeconds     = 20
)

// errForbidden is the single denial used for a denied permission. The message
// does not say which permission or which role would have worked.
var errForbidden = huma.Error403Forbidden("You do not have permission for this action.")

// ---------- helpers ----------

func unwrapRequest(hctx huma.Context) (*http.Request, error) {
	if hctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(hctx)
	if r == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	return r, nil
}

// pluginContext returns the mount context, or nil before a real mount.
func (p *Plugin) pluginContext() *plugin.Context {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.ctx
}

// allowed asks the host permission resolver. It fails closed: an unwired host,
// a nil session, or a nil mounted context denies the request.
func (p *Plugin) allowed(r *http.Request, permKey, minRole string) bool {
	ctx := p.pluginContext()
	if ctx == nil {
		return false
	}
	return ctx.HasPerm(r, permKey, minRole)
}

// requireOrg resolves the workspace of an authenticated request. A zero value
// means the request never passed the session guard; the caller must refuse it.
func (p *Plugin) requireOrg(r *http.Request) (uint, error) {
	orgID := plugin.OrgIDFromContext(r.Context())
	if orgID == 0 {
		return 0, huma.Error401Unauthorized("Authentication required. Sign in to continue.")
	}
	return orgID, nil
}

// humaErrorFor maps infrastructure errors to responses. A missing vault or
// tenant handle is a server fault; a missing row is a 404 that also covers
// cross-workspace ids, so existence never leaks.
func (p *Plugin) humaErrorFor(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("Tracker not found.")
	case errors.Is(err, errTenantUnavailable):
		return huma.Error503ServiceUnavailable("This server cannot reach workspace data. Contact your administrator.")
	case errors.Is(err, errCryptoUnavailable):
		return huma.Error503ServiceUnavailable("The credential vault is unavailable. Contact your administrator.")
	default:
		return huma.Error500InternalServerError("The request failed. Try again.")
	}
}

// newSecretID returns the immutable per-tracker vault subject.
func newSecretID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("pt: generate secret id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// normalizeBaseURL validates and trims a site URL. Only absolute http(s) URLs
// without user info or query are accepted: the URL is later used as an SSRF
// target by the plugin itself.
func normalizeBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", huma.Error422UnprocessableEntity("Enter the site address.")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", huma.Error422UnprocessableEntity("The site address is not a valid URL.")
	}
	switch {
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return "", huma.Error422UnprocessableEntity("The site address must start with http:// or https://.")
	case parsed.Host == "" || !strings.Contains(parsed.Host, "."):
		return "", huma.Error422UnprocessableEntity("The site address must include a domain name.")
	case parsed.User != nil:
		return "", huma.Error422UnprocessableEntity("Remove the account data from the site address.")
	case parsed.RawQuery != "" || parsed.Fragment != "":
		return "", huma.Error422UnprocessableEntity("Use the site root address, without a query or fragment.")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if parsed.Path == "" {
		parsed.Path = ""
	}
	return parsed.String(), nil
}

// normalizeAPIRawURL validates an optional API base URL.
func normalizeAPIRawURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	base, err := normalizeBaseURL(raw)
	if err != nil {
		return "", err
	}
	return base, nil
}

// validateTimezone accepts an IANA name or the empty string.
func validateTimezone(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if _, err := time.LoadLocation(trimmed); err != nil {
		return "", huma.Error422UnprocessableEntity("The time zone is not a known name, for example Asia/Shanghai.")
	}
	return trimmed, nil
}

// validateSigninWindow rejects a window whose start and end are equal but not
// both zero: such a window is either "any time" or "never", and guessing is
// worse than refusing.
func validateSigninWindow(start, end int) error {
	if start == 0 && end == 0 {
		return nil
	}
	if start == end {
		return huma.Error422UnprocessableEntity("Set a different start and end time, or leave both empty.")
	}
	return nil
}

// cleanCookie validates a pasted Cookie header. Header injection through CRLF
// and oversized values are refused rather than truncated.
func cleanCookie(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > cookieMaxBytes {
		return "", huma.Error422UnprocessableEntity("The cookie is too large. Paste only the site cookies.")
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return "", huma.Error422UnprocessableEntity("The cookie contains a line break. Paste it as one line.")
	}
	if !strings.Contains(trimmed, "=") {
		return "", huma.Error422UnprocessableEntity("The cookie has no name=value pair. Copy it from the site's request headers.")
	}
	if len(adapter.ParseCookieHeader(trimmed)) == 0 {
		return "", huma.Error422UnprocessableEntity("The cookie could not be read. Copy it again from the browser.")
	}
	return trimmed, nil
}

// applyTrackerBody copies the optional settings from a request body onto a
// tracker. It never touches secrets or SecretID.
func applyTrackerBody(t *PTTracker, body trackerWriteBody) error {
	if v := strings.TrimSpace(body.Name); v != "" {
		t.Name = v
	}
	if v := strings.TrimSpace(body.Username); body.Username != "" {
		t.Username = v
	}
	if v := strings.TrimSpace(body.UserAgent); v != "" {
		t.UserAgent = v
	}
	if body.Timezone != "" {
		tz, err := validateTimezone(body.Timezone)
		if err != nil {
			return err
		}
		t.Timezone = tz
	}
	if body.APIBaseURL != "" {
		apiBase, err := normalizeAPIRawURL(body.APIBaseURL)
		if err != nil {
			return err
		}
		t.APIBaseURL = apiBase
	}
	if body.Enabled != nil {
		t.Enabled = *body.Enabled
	}
	if body.SigninEnabled != nil {
		t.SigninEnabled = *body.SigninEnabled
	}
	if body.SigninWindowStartMin != nil {
		t.SigninWindowStartMin = *body.SigninWindowStartMin
	}
	if body.SigninWindowEndMin != nil {
		t.SigninWindowEndMin = *body.SigninWindowEndMin
	}
	if err := validateSigninWindow(t.SigninWindowStartMin, t.SigninWindowEndMin); err != nil {
		return err
	}
	if body.StatsIntervalHours != nil {
		t.StatsIntervalHours = *body.StatsIntervalHours
	}
	if body.TimeoutSeconds != nil {
		t.TimeoutSeconds = *body.TimeoutSeconds
	}
	if body.LimitIntervalSeconds != nil {
		t.LimitIntervalSeconds = *body.LimitIntervalSeconds
	}
	if body.LimitCount != nil {
		t.LimitCount = *body.LimitCount
	}
	if body.LimitSeconds != nil {
		t.LimitSeconds = *body.LimitSeconds
	}
	return nil
}

// bodySecrets builds a secret payload from a write body.
func bodySecrets(body trackerWriteBody) (storedSecret, error) {
	cookie, err := cleanCookie(body.Cookie)
	if err != nil {
		return storedSecret{}, err
	}
	return storedSecret{
		Login:  strings.TrimSpace(body.LoginSecret),
		Cookie: cookie,
		APIKey: strings.TrimSpace(body.APIKey),
	}, nil
}

// verifySecrets proves a candidate secret against the site before it is stored.
func (p *Plugin) verifySecrets(ctx context.Context, t *PTTracker, secret storedSecret) (*adapter.LoginResult, error) {
	ad, err := p.registry().Get(adapter.Schema(t.SchemaKey))
	if err != nil {
		return nil, err
	}
	site := siteOf(t)
	client, err := adapter.NewClient(p.client(site.Timeout), site, secret.Cookie)
	if err != nil {
		return nil, err
	}
	return ad.CheckSession(ctx, &adapter.Session{
		Site: site,
		Secret: adapter.Secret{
			Login:  secret.Login,
			Cookie: secret.Cookie,
			APIKey: secret.APIKey,
			Token:  secret.Token,
		},
		Client: client,
		Now:    p.clock(),
	})
}

// buildView assembles the API view for one tracker.
func (p *Plugin) buildView(ctx context.Context, t *PTTracker) trackerView {
	view := trackerView{PTTracker: *t}

	ad, err := p.registry().Get(adapter.Schema(t.SchemaKey))
	if err == nil {
		view.SchemaLabel = ad.DisplayName()
		view.Capabilities = ad.Capabilities()
	} else {
		view.SchemaLabel = t.SchemaKey
	}

	if cred, err := p.credentialFor(ctx, t.OrgID, t.ID); err == nil && cred != nil {
		view.Credential = credentialView{
			HasLoginSecret: cred.HasLoginSecret,
			HasCookie:      cred.HasCookie,
			HasAPIKey:      cred.HasAPIKey,
			SecretVersion:  cred.SecretVersion,
			CookieSetAt:    cred.CookieSetAt,
			LastLoginAt:    cred.LastLoginAt,
		}
	}
	if snap, err := p.latestSnapshot(ctx, t.OrgID, t.ID); err == nil && snap != nil {
		view.LatestSnapshot = snap
	}
	if last, err := p.latestSignin(ctx, t.OrgID, t.ID); err == nil && last != nil {
		view.LastSignin = last
	}
	return view
}

// summarize aggregates the visible trackers without inventing values.
func summarize(views []trackerView) summaryView {
	out := summaryView{TrackerCount: len(views)}
	var uploaded, downloaded int64
	var uploadSeen, downloadSeen bool
	var bonus float64
	var bonusSeen bool

	for i := range views {
		view := &views[i]
		if view.Enabled {
			out.EnabledCount++
		}
		if !view.CookieValid {
			out.CookieInvalidCount++
		}
		if view.NeedsManualAction {
			out.ManualActionCount++
		}
		if snap := view.LatestSnapshot; snap != nil {
			if snap.UploadedBytes != nil {
				uploaded += *snap.UploadedBytes
				uploadSeen = true
			}
			if snap.DownloadedBytes != nil {
				downloaded += *snap.DownloadedBytes
				downloadSeen = true
			}
			if snap.Bonus != nil {
				bonus += *snap.Bonus
				bonusSeen = true
			}
			if snap.WarningCount != nil {
				out.WarningCount += *snap.WarningCount
			}
		}
	}

	if uploadSeen {
		out.UploadedBytes = &uploaded
	}
	if downloadSeen {
		out.DownloadedBytes = &downloaded
	}
	if uploadSeen && downloadSeen {
		switch {
		case downloaded == 0 && uploaded > 0:
			out.RatioInfinite = true
		case downloaded > 0:
			ratio := float64(uploaded) / float64(downloaded)
			out.Ratio = &ratio
		}
	}
	if bonusSeen {
		out.Bonus = &bonus
	}
	return out
}

// trackersOf loads every tracker of one workspace as views.
func (p *Plugin) trackersOf(ctx context.Context, orgID uint) ([]trackerView, error) {
	list, err := p.listTrackers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]trackerView, 0, len(list))
	for i := range list {
		out = append(out, p.buildView(ctx, &list[i]))
	}
	return out, nil
}

// loadTracker loads one tracker after the permission check.
func (p *Plugin) loadTracker(ctx context.Context, orgID, id uint) (*PTTracker, error) {
	t, err := p.findTracker(ctx, orgID, id)
	if err != nil {
		return nil, p.humaErrorFor(err)
	}
	return t, nil
}

// ---------- handlers ----------

func (p *Plugin) listTrackersHandler(ctx context.Context, input *listTrackersInput) (*listTrackersOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permRead, roleMember) {
		return nil, errForbidden
	}
	views, err := p.trackersOf(ctx, orgID)
	if err != nil {
		return nil, p.humaErrorFor(err)
	}

	out := &listTrackersOutput{}
	out.Body.Trackers = views
	out.Body.Summary = summarize(views)
	return out, nil
}

func (p *Plugin) getOverview(ctx context.Context, input *overviewInput) (*overviewOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permRead, roleMember) {
		return nil, errForbidden
	}
	views, err := p.trackersOf(ctx, orgID)
	if err != nil {
		return nil, p.humaErrorFor(err)
	}
	return &overviewOutput{Body: overviewView{
		Summary:            summarize(views),
		Schemas:            p.registry().List(),
		SchedulerAvailable: p.schedulerAvailable(),
		ServerTimezone:     time.Local.String(),
	}}, nil
}

func (p *Plugin) getTrackerHandler(ctx context.Context, input *trackerInput) (*trackerOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permRead, roleMember) {
		return nil, errForbidden
	}
	t, err := p.loadTracker(ctx, orgID, input.ID)
	if err != nil {
		return nil, err
	}
	view := p.buildView(ctx, t)
	return &trackerOutput{Body: view}, nil
}

func (p *Plugin) createTrackerHandler(ctx context.Context, input *createTrackerInput) (*trackerOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permCreate, roleAdmin) {
		return nil, errForbidden
	}
	body := input.Body

	name := strings.TrimSpace(body.Name)
	if name == "" {
		return nil, huma.Error422UnprocessableEntity("Enter a name for this site.")
	}
	if len(name) > 120 {
		return nil, huma.Error422UnprocessableEntity("Use a name of 120 characters or less.")
	}

	ad, err := p.registry().Get(adapter.Schema(strings.TrimSpace(body.Schema)))
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("Choose a supported site type.")
	}

	base, err := normalizeBaseURL(body.BaseURL)
	if err != nil {
		return nil, err
	}
	apiBase, err := normalizeAPIRawURL(body.APIBaseURL)
	if err != nil {
		return nil, err
	}
	tz, err := validateTimezone(body.Timezone)
	if err != nil {
		return nil, err
	}
	secret, err := bodySecrets(body)
	if err != nil {
		return nil, err
	}

	if _, findErr := p.findTrackerByName(ctx, orgID, name); findErr == nil {
		return nil, huma.Error409Conflict("A tracker with this name already exists.")
	} else if !errors.Is(findErr, ErrNotFound) {
		return nil, p.humaErrorFor(findErr)
	}

	secretID, err := newSecretID()
	if err != nil {
		return nil, huma.Error500InternalServerError("The request failed. Try again.")
	}

	defaults := ad.Defaults()
	caps := ad.Capabilities()
	t := &PTTracker{
		OrgID:                orgID,
		Name:                 name,
		SecretID:             secretID,
		BaseURL:              base,
		APIBaseURL:           apiBase,
		Domain:               adapter.NormalizeDomain(base),
		SchemaKey:            string(ad.Schema()),
		Username:             strings.TrimSpace(body.Username),
		UserAgent:            firstNonEmpty(strings.TrimSpace(body.UserAgent), defaults.UserAgent, userAgent),
		Timezone:             tz,
		Enabled:              true,
		SigninEnabled:        caps.Signin,
		StatsIntervalHours:   defaultStatsIntervalHours,
		TimeoutSeconds:       defaultsOr(defaults.TimeoutSeconds, defaultTimeoutSeconds),
		LimitIntervalSeconds: defaults.LimitIntervalSeconds,
		LimitCount:           defaults.LimitCount,
		LimitSeconds:         defaults.LimitSeconds,
		CreatedByUserID:      plugin.UserIDFromContext(r.Context()),
	}
	if err := applyTrackerBody(t, body); err != nil {
		return nil, err
	}

	// Verify a supplied cookie or API key before any row is written, so a bad
	// paste never leaves a tracker that quietly fails every night.
	if verify, verifyErr := p.shouldVerify(caps, secret); verifyErr != nil {
		return nil, verifyErr
	} else if verify {
		res, checkErr := p.verifySecrets(ctx, t, secret)
		if checkErr != nil || res == nil || !res.LoggedIn {
			return nil, huma.Error422UnprocessableEntity(verificationMessage(checkErr))
		}
		t.CookieValid = true
	}

	if err := p.createTracker(ctx, t); err != nil {
		return nil, p.humaErrorFor(err)
	}

	if !secret.empty() {
		if err := p.storeSecrets(ctx, t, secret, true); err != nil {
			_ = p.deleteTrackerCascade(ctx, orgID, t.ID)
			_ = p.shredSecret(orgID, t.SecretID)
			return nil, p.humaErrorFor(err)
		}
	}

	now := p.clock()
	updates := map[string]any{"next_stats_at": planStatsAt(t, now)}
	if t.SigninEnabled && caps.Signin {
		updates["next_signin_at"] = planSigninAt(t, now)
	}
	p.patchTracker(ctx, t, updates)

	return &trackerOutput{Body: p.buildView(ctx, t)}, nil
}

// shouldVerify reports whether the candidate secret can be proven right now.
func (p *Plugin) shouldVerify(caps adapter.Capabilities, secret storedSecret) (bool, error) {
	if secret.APIKey != "" && !caps.APIKeyAuth {
		return false, huma.Error422UnprocessableEntity("This site type does not use an API key.")
	}
	if secret.Cookie != "" && !caps.CookieAuth {
		return false, huma.Error422UnprocessableEntity("This site type does not accept a pasted cookie.")
	}
	if secret.Cookie != "" && caps.CookieAuth {
		return true, nil
	}
	if secret.APIKey != "" && caps.APIKeyAuth {
		return true, nil
	}
	return false, nil
}

// storeSecrets encrypts and saves the payload. verified records whether the
// site accepted it, which sets the tracker's cookie state.
func (p *Plugin) storeSecrets(ctx context.Context, t *PTTracker, secret storedSecret, verified bool) error {
	cipher, err := p.encryptSecret(t.OrgID, t.SecretID, secret)
	if err != nil {
		return err
	}
	now := p.clock()

	cred, findErr := p.credentialFor(ctx, t.OrgID, t.ID)
	switch {
	case findErr == nil && cred != nil:
		cred.Ciphertext = cipher
		cred.SecretVersion++
		cred.HasLoginSecret = secret.Login != ""
		cred.HasCookie = secret.Cookie != ""
		cred.HasAPIKey = secret.APIKey != ""
		if secret.Cookie != "" {
			cred.CookieSetAt = &now
		}
		cred.UpdatedAt = now
		if err := p.saveCredential(ctx, cred); err != nil {
			return err
		}
	case errors.Is(findErr, ErrNotFound):
		cred = &PTCredential{
			OrgID:          t.OrgID,
			TrackerID:      t.ID,
			Ciphertext:     cipher,
			SecretVersion:  1,
			HasLoginSecret: secret.Login != "",
			HasCookie:      secret.Cookie != "",
			HasAPIKey:      secret.APIKey != "",
		}
		if secret.Cookie != "" {
			cred.CookieSetAt = &now
		}
		if err := p.createCredential(ctx, cred); err != nil {
			return err
		}
	default:
		return findErr
	}

	if verified {
		p.patchTracker(ctx, t, map[string]any{
			"cookie_valid":         true,
			"consecutive_failures": 0,
			"last_error":           "",
			"last_error_at":        nil,
			"needs_manual_action":  false,
			"manual_reason":        "",
		})
		t.CookieValid = true
	}
	return nil
}

func (p *Plugin) updateTrackerHandler(ctx context.Context, input *updateTrackerInput) (*trackerOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permUpdate, roleAdmin) {
		return nil, errForbidden
	}
	body := input.Body
	if strings.TrimSpace(body.LoginSecret) != "" || strings.TrimSpace(body.Cookie) != "" || strings.TrimSpace(body.APIKey) != "" {
		return nil, huma.Error422UnprocessableEntity("Change credentials through the credential endpoint.")
	}

	t, err := p.loadTracker(ctx, orgID, input.ID)
	if err != nil {
		return nil, err
	}

	if v := strings.TrimSpace(body.Name); v != "" && v != t.Name {
		if _, findErr := p.findTrackerByName(ctx, orgID, v); findErr == nil {
			return nil, huma.Error409Conflict("A tracker with this name already exists.")
		} else if !errors.Is(findErr, ErrNotFound) {
			return nil, p.humaErrorFor(findErr)
		}
	}
	if err := applyTrackerBody(t, body); err != nil {
		return nil, err
	}
	if body.BaseURL != "" {
		base, err := normalizeBaseURL(body.BaseURL)
		if err != nil {
			return nil, err
		}
		t.BaseURL = base
		t.Domain = adapter.NormalizeDomain(base)
	}
	t.UpdatedAt = p.clock()
	if err := p.saveTracker(ctx, t); err != nil {
		return nil, p.humaErrorFor(err)
	}

	// Re-plan the next runs so a changed window or interval takes effect now
	// instead of at the end of the old plan.
	now := p.clock()
	updates := map[string]any{
		"next_stats_at":  planStatsAt(t, now),
		"next_signin_at": nil,
	}
	if t.SigninEnabled {
		updates["next_signin_at"] = planSigninAt(t, now)
	}
	p.patchTracker(ctx, t, updates)

	return &trackerOutput{Body: p.buildView(ctx, t)}, nil
}

func (p *Plugin) deleteTrackerHandler(ctx context.Context, input *trackerInput) (*deleteOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permDelete, roleAdmin) {
		return nil, errForbidden
	}
	t, err := p.loadTracker(ctx, orgID, input.ID)
	if err != nil {
		return nil, err
	}
	if err := p.deleteTrackerCascade(ctx, orgID, t.ID); err != nil {
		return nil, p.humaErrorFor(err)
	}
	// The rows are gone; a shred failure leaves an orphan key with no
	// ciphertext, so the deletion still stands.
	_ = p.shredSecret(orgID, t.SecretID)
	return &deleteOutput{Body: map[string]bool{"ok": true}}, nil
}

func (p *Plugin) setCredentialHandler(ctx context.Context, input *credentialInput) (*credentialOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permUpdate, roleAdmin) {
		return nil, errForbidden
	}
	t, err := p.loadTracker(ctx, orgID, input.ID)
	if err != nil {
		return nil, err
	}

	body := input.Body
	cookie, err := cleanCookie(body.Cookie)
	if err != nil {
		return nil, err
	}
	login := strings.TrimSpace(body.LoginSecret)
	apiKey := strings.TrimSpace(body.APIKey)
	if login == "" && cookie == "" && apiKey == "" {
		return nil, huma.Error422UnprocessableEntity("Enter a new secret, cookie, or API key.")
	}

	ad, err := p.registry().Get(adapter.Schema(t.SchemaKey))
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("Choose a supported site type.")
	}
	caps := ad.Capabilities()
	if cookie != "" && !caps.CookieAuth {
		return nil, huma.Error422UnprocessableEntity("This site type does not accept a pasted cookie.")
	}
	if apiKey != "" && !caps.APIKeyAuth {
		return nil, huma.Error422UnprocessableEntity("This site type does not use an API key.")
	}

	current, err := p.readSecrets(ctx, t)
	if err != nil {
		return nil, p.humaErrorFor(err)
	}
	merged := current
	if login != "" {
		merged.Login = login
	}
	if cookie != "" {
		merged.Cookie = cookie
	}
	if apiKey != "" {
		merged.APIKey = apiKey
	}

	needsProof := cookie != "" || (apiKey != "" && caps.APIKeyAuth)
	verified := false
	var proofErr error
	if needsProof {
		res, checkErr := p.verifySecrets(ctx, t, merged)
		if checkErr == nil && res != nil && res.LoggedIn {
			verified = true
		} else {
			proofErr = checkErr
		}
	}

	if needsProof && !verified && !body.Force {
		return &credentialOutput{Body: credentialWriteView{
			Stored:      false,
			Verified:    false,
			Outcome:     string(adapter.OutcomeAuthFailed),
			Reason:      reasonFromError(proofErr),
			Message:     verificationMessage(proofErr),
			CookieValid: t.CookieValid,
		}}, nil
	}

	if err := p.storeSecrets(ctx, t, merged, verified); err != nil {
		return nil, p.humaErrorFor(err)
	}
	if !verified {
		p.patchTracker(ctx, t, map[string]any{"cookie_valid": false})
		t.CookieValid = false
	}

	out := &credentialOutput{Body: credentialWriteView{
		Stored:      true,
		Verified:    verified,
		Outcome:     string(adapter.OutcomeSuccess),
		CookieValid: t.CookieValid,
		Message:     "The credential is saved.",
	}}
	if !verified {
		out.Body.Outcome = string(adapter.OutcomeLoginOKUnconfirmed)
		out.Body.Reason = "unverified"
		out.Body.Message = "The credential is saved. The site has not accepted it yet."
	}
	return out, nil
}

// readSecrets decrypts the stored payload. A missing row is an empty payload.
func (p *Plugin) readSecrets(ctx context.Context, t *PTTracker) (storedSecret, error) {
	cred, err := p.credentialFor(ctx, t.OrgID, t.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return storedSecret{}, nil
		}
		return storedSecret{}, err
	}
	return p.decryptSecret(t.OrgID, t.SecretID, cred.Ciphertext)
}

func (p *Plugin) loginHandler(ctx context.Context, input *trackerInput) (*actionOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permUpdate, roleAdmin) {
		return nil, errForbidden
	}
	t, err := p.loadTracker(ctx, orgID, input.ID)
	if err != nil {
		return nil, err
	}

	out, err := p.login(ctx, t)
	if err != nil {
		if mapped := p.actionError(err); mapped != nil {
			return nil, mapped
		}
		return &actionOutput{Body: p.failedOutcome(t, err)}, nil
	}
	return &actionOutput{Body: *out}, nil
}

func (p *Plugin) signinHandler(ctx context.Context, input *signinInput) (*actionOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permSignin, roleMember) {
		return nil, errForbidden
	}
	t, err := p.loadTracker(ctx, orgID, input.ID)
	if err != nil {
		return nil, err
	}

	res, err := p.signin(ctx, t, triggerManual, input.Body.Force)
	if err != nil {
		if mapped := p.actionError(err); mapped != nil {
			return nil, mapped
		}
		return &actionOutput{Body: p.failedOutcome(t, err)}, nil
	}
	return &actionOutput{Body: p.outcomeFrom(t, res)}, nil
}

func (p *Plugin) refreshHandler(ctx context.Context, input *trackerInput) (*actionOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permRefresh, roleMember) {
		return nil, errForbidden
	}
	t, err := p.loadTracker(ctx, orgID, input.ID)
	if err != nil {
		return nil, err
	}

	if _, err := p.refresh(ctx, t, triggerManual); err != nil {
		if mapped := p.actionError(err); mapped != nil {
			return nil, mapped
		}
		return &actionOutput{Body: p.failedOutcome(t, err)}, nil
	}
	out := p.outcomeFrom(t, nil)
	out.Outcome = string(adapter.OutcomeSuccess)
	out.Message = "The statistics are updated."
	return &actionOutput{Body: out}, nil
}

// actionError maps an action failure. Infrastructure faults become HTTP
// errors; business verdicts are reported as a 200 response with an outcome, so
// a rejected credential is not mistaken for a broken server.
func (p *Plugin) actionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errTenantUnavailable), errors.Is(err, errCryptoUnavailable):
		return p.humaErrorFor(err)
	case errors.Is(err, ErrNotFound):
		return p.humaErrorFor(err)
	}
	var deferred *deferredError
	if errors.As(err, &deferred) {
		return huma.Error429TooManyRequests("The site needs a pause. Try again later.")
	}
	if errors.Is(err, errNoCredential) {
		return nil
	}
	return nil
}

// outcomeFrom renders the tracker's current state as an action result. A
// sign-in record supplies the verdict when there is one.
func (p *Plugin) outcomeFrom(t *PTTracker, res *PTSigninResult) actionOutcome {
	out := actionOutcome{
		CookieValid:  t.CookieValid,
		AttemptedAt:  p.clock(),
		NextSigninAt: t.NextSigninAt,
		NextStatsAt:  t.NextStatsAt,
	}
	if res != nil {
		out.Outcome = res.Outcome
		out.Reason = res.Reason
		out.Message = res.Message
		out.BonusAwarded = res.BonusAwarded
		out.AttemptedAt = res.AttemptedAt
		return out
	}
	switch {
	case t.NeedsManualAction:
		out.Outcome = string(adapter.OutcomeNeedsManualAction)
		out.Reason = t.ManualReason
		out.Message = firstNonEmpty(t.LastError, manualMessage(t.ManualReason))
	default:
		out.Outcome = string(adapter.OutcomeAuthFailed)
		out.Reason = "failed"
		out.Message = firstNonEmpty(t.LastError, "The action did not complete.")
	}
	return out
}

// failedOutcome renders an action error as a user-visible verdict.
func (p *Plugin) failedOutcome(t *PTTracker, err error) actionOutcome {
	return actionOutcome{
		Outcome:      outcomeFromError(err),
		Reason:       reasonFromError(err),
		Message:      messageFromError(err),
		CookieValid:  t.CookieValid,
		AttemptedAt:  p.clock(),
		NextSigninAt: t.NextSigninAt,
		NextStatsAt:  t.NextStatsAt,
	}
}

func (p *Plugin) snapshotsHandler(ctx context.Context, input *snapshotsInput) (*snapshotsOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permRead, roleMember) {
		return nil, errForbidden
	}
	t, err := p.loadTracker(ctx, orgID, input.ID)
	if err != nil {
		return nil, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 90
	}
	rows, err := p.listSnapshots(ctx, orgID, t.ID, limit)
	if err != nil {
		return nil, p.humaErrorFor(err)
	}
	return &snapshotsOutput{Body: rows}, nil
}

func (p *Plugin) signinsHandler(ctx context.Context, input *signinsInput) (*signinsOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permRead, roleMember) {
		return nil, errForbidden
	}
	t, err := p.loadTracker(ctx, orgID, input.ID)
	if err != nil {
		return nil, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 30
	}
	rows, err := p.listSignins(ctx, orgID, t.ID, limit)
	if err != nil {
		return nil, p.humaErrorFor(err)
	}
	return &signinsOutput{Body: rows}, nil
}

var (
	// loginFormRe finds a form that posts a username field.
	loginFormRe = regexp.MustCompile(`(?is)<form[^>]*>(.*?)</form>`)
	captchaRe   = regexp.MustCompile(`(?i)(imagehash|imagestring|verify_code|captcha|验证码)`)
)

func (p *Plugin) detectHandler(ctx context.Context, input *detectInput) (*detectOutput, error) {
	r, err := unwrapRequest(input.Ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := p.requireOrg(r)
	if err != nil {
		return nil, err
	}
	if !p.allowed(r, permCreate, roleAdmin) {
		return nil, errForbidden
	}
	_ = orgID

	base, err := normalizeBaseURL(input.Body.URL)
	if err != nil {
		return nil, err
	}

	out := &detectOutput{}
	if schema, ok := p.registry().Suggest(base); ok {
		out.Body.SuggestedSchema = string(schema)
	}

	site := adapter.Site{BaseURL: base, Timeout: defaultTimeoutSeconds * time.Second}
	client, err := adapter.NewClient(p.client(site.Timeout), site, "")
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("The site address is not a valid URL.")
	}

	page, err := client.Get(ctx, base, nil)
	if err != nil {
		if errors.Is(err, adapter.ErrCloudflare) {
			out.Body.Challenge = true
			out.Body.Message = "The site shows an anti-bot challenge. Save the site and paste a browser cookie."
			return out, nil
		}
		out.Body.Message = "The site could not be reached. Check the address and the network."
		return out, nil
	}

	out.Body.Reachable = true
	out.Body.HTTPStatus = page.Status
	body := string(page.Body)
	for _, form := range loginFormRe.FindAllStringSubmatch(body, -1) {
		inner := form[1]
		if strings.Contains(strings.ToLower(inner), "username") || strings.Contains(inner, "name=\"") {
			out.Body.HasLoginForm = true
			break
		}
	}
	if captchaRe.MatchString(body) {
		out.Body.HasCaptcha = true
	}
	out.Body.Message = detectionMessage(out.Body)
	return out, nil
}

func detectionMessage(view detectView) string {
	switch {
	case view.Challenge:
		return "The site shows an anti-bot challenge. Save the site and paste a browser cookie."
	case !view.Reachable:
		return "The site could not be reached. Check the address and the network."
	case view.HasCaptcha:
		return "The login page has a captcha. Save the site and paste a browser cookie."
	case view.HasLoginForm && view.SuggestedSchema != "":
		return "The page matches the suggested site type. Confirm it before saving."
	case view.HasLoginForm:
		return "The page has a login form. Choose the site type that matches this tracker."
	default:
		return "The page has no login form. The address may be the API host or a public page."
	}
}

// verificationMessage turns a probe failure into a sentence for the form.
func verificationMessage(err error) string {
	if err == nil {
		return "The site did not accept the credential. Check it and try again."
	}
	switch {
	case errors.Is(err, adapter.ErrCookieGone), errors.Is(err, adapter.ErrAuth):
		return "The site did not accept the credential. Check it and try again."
	case errors.Is(err, adapter.ErrCloudflare), errors.Is(err, adapter.ErrCaptcha):
		return manualMessage(adapter.ManualReasonOf(err))
	case errors.Is(err, adapter.ErrNetwork):
		return "The site could not be reached. Check the network and try again."
	case errors.Is(err, adapter.ErrConfig):
		return "The site settings are incomplete. Check the address and the site type."
	default:
		return "The site did not accept the credential. Check it and try again."
	}
}

func defaultsOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
