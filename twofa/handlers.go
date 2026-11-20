package twofa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
)

func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	v, _ := meta[key].(string)
	return v
}

func metaInt(meta map[string]any, key string, fallback int) int {
	if meta == nil {
		return fallback
	}
	switch v := meta[key].(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	}
	return fallback
}

func metaBool(meta map[string]any, key string) bool {
	if meta == nil {
		return false
	}
	v, _ := meta[key].(bool)
	return v
}

func metaToAccount(item plugin.VaultItemMeta) TwoFAAccount {
	var tags string
	var hasTagsKey bool
	if item.Metadata != nil {
		if raw, ok := item.Metadata["tags"]; ok {
			hasTagsKey = true
			tags, _ = raw.(string)
		}
	}
	if !hasTagsKey && len(item.Tags) > 0 {
		tags = strings.Join(item.Tags, ", ")
	}
	algo := metaString(item.Metadata, "algorithm")
	if algo == "" {
		algo = "SHA1"
	}
	return TwoFAAccount{
		ID:        item.ID,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
		OrgID:     item.OrgID,
		Name:      item.Name,
		Issuer:    metaString(item.Metadata, "issuer"),
		Account:   metaString(item.Metadata, "account"),
		Algorithm: algo,
		Digits:    metaInt(item.Metadata, "digits", 6),
		Period:    metaInt(item.Metadata, "period", 30),
		Tags:      tags,
		Notes:     metaString(item.Metadata, "notes"),
		Pinned:    metaBool(item.Metadata, "pinned"),
	}
}

func metaToSummary(item plugin.VaultItemMeta, code string, rem int) AccountSummary {
	acc := metaToAccount(item)
	return AccountSummary{
		ID:               acc.ID,
		CreatedAt:        acc.CreatedAt,
		UpdatedAt:        acc.UpdatedAt,
		OrgID:            acc.OrgID,
		Name:             acc.Name,
		Issuer:           acc.Issuer,
		Account:          acc.Account,
		Algorithm:        acc.Algorithm,
		Digits:           acc.Digits,
		Period:           acc.Period,
		Tags:             acc.Tags,
		Notes:            acc.Notes,
		Pinned:           acc.Pinned,
		CurrentCode:      code,
		SecondsRemaining: rem,
	}
}

func buildTwoFAMetadata(acc *TwoFAAccount) map[string]any {
	return map[string]any{
		"issuer":    acc.Issuer,
		"account":   acc.Account,
		"algorithm": acc.Algorithm,
		"digits":    acc.Digits,
		"period":    acc.Period,
		"tags":      acc.Tags,
		"notes":     acc.Notes,
		"pinned":    acc.Pinned,
	}
}

func buildTwoFAPayload(acc *TwoFAAccount, secret string) map[string]any {
	return map[string]any{
		"issuer":     acc.Issuer,
		"account":    acc.Account,
		"secret":     secret,
		"otpauthUri": BuildOTPAuthURI(acc, secret),
	}
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var out []string
	for _, part := range parts {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func extractSecretFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if s, ok := payload["secret"].(string); ok && strings.TrimSpace(s) != "" {
		return NormalizeSecret(s)
	}
	if uri, ok := payload["otpauthUri"].(string); ok && strings.TrimSpace(uri) != "" {
		if _, s, err := ParseOTPAuthURI(uri); err == nil {
			return s
		}
	}
	return ""
}

// errPersonalSpaceUnavailable fails the personal routes closed when the caller
// has no personal org. Falling back to the session workspace would put private
// seeds in the team vault.
var errPersonalSpaceUnavailable = errors.New("Personal space is unavailable for this account.")

// errNoWorkspace means the request carries no team workspace.
var errNoWorkspace = errors.New("unauthorized workspace")

// vaultScope names the vault a request reads and writes. The team vault is
// org-scoped and admin-gated; the personal vault is user-scoped and owned by
// the caller, so ownership alone is the boundary.
type vaultScope struct {
	OrgID   uint
	Scope   plugin.VaultScope
	OwnerID uint // 0 for the team vault
	Team    bool
}

// scopeFor resolves the request path to its vault. It never falls back from a
// missing personal org to the session workspace.
func (p *Plugin) scopeFor(r *http.Request) (vaultScope, error) {
	if strings.HasPrefix(r.URL.Path, "/api/personal/twofa/") {
		ownerID := p.actorUserID(r)
		if ownerID == 0 {
			return vaultScope{}, errPersonalSpaceUnavailable
		}
		var orgID uint
		if p.host != nil && p.host.Session() != nil {
			orgID = p.host.Session().PersonalOrgID(r)
		}
		if orgID == 0 {
			return vaultScope{}, errPersonalSpaceUnavailable
		}
		return vaultScope{OrgID: orgID, Scope: plugin.VaultScopeUser, OwnerID: ownerID}, nil
	}
	orgID := p.orgID(r)
	if orgID == 0 {
		return vaultScope{}, errNoWorkspace
	}
	return vaultScope{OrgID: orgID, Scope: plugin.VaultScopeOrg, Team: true}, nil
}

// requireScope writes the failure response and reports whether the handler may
// continue.
func (p *Plugin) requireScope(w http.ResponseWriter, r *http.Request) (vaultScope, bool) {
	scope, err := p.scopeFor(r)
	if err == nil {
		return scope, true
	}
	if errors.Is(err, errPersonalSpaceUnavailable) {
		http.Error(w, err.Error(), http.StatusForbidden)
		return vaultScope{}, false
	}
	http.Error(w, err.Error(), http.StatusUnauthorized)
	return vaultScope{}, false
}

// requireTwoFAItem loads an item only when it belongs to the active scope. The
// owner check is what keeps one member's personal item invisible to another
// member in the same personal org.
func (p *Plugin) requireTwoFAItem(ctx context.Context, vs plugin.VaultService, scope vaultScope, id uint) (*plugin.VaultItemMeta, error) {
	meta, err := vs.GetMeta(ctx, scope.OrgID, id)
	if err != nil {
		return nil, err
	}
	if meta == nil || meta.Scope != scope.Scope || meta.ItemType != "twofa_totp" {
		return nil, plugin.ErrSecretNotFound
	}
	if scope.Scope == plugin.VaultScopeUser && meta.OwnerUserID != scope.OwnerID {
		return nil, plugin.ErrSecretNotFound
	}
	return meta, nil
}

// listAccounts returns safe summaries for every 2FA account in the active
// scope, including real-time TOTP codes and time remaining.
func (p *Plugin) listAccounts(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	items, err := vs.ListItems(r.Context(), scope.OrgID, plugin.VaultFilter{
		Scope:       scope.Scope,
		ItemType:    "twofa_totp",
		OwnerUserID: scope.OwnerID,
	})
	if err != nil {
		http.Error(w, "failed to query accounts", http.StatusInternalServerError)
		return
	}

	summaries := make([]AccountSummary, 0, len(items))
	for _, item := range items {
		code, rem, cErr := vs.ComputeTOTP(r.Context(), scope.OrgID, item.ID)
		if cErr != nil {
			code = ""
			rem = 0
		}
		summaries = append(summaries, metaToSummary(item, code, rem))
	}

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Pinned != summaries[j].Pinned {
			return summaries[i].Pinned
		}
		if summaries[i].Name != summaries[j].Name {
			return summaries[i].Name < summaries[j].Name
		}
		return summaries[i].ID < summaries[j].ID
	})

	writeJSON(w, summaries)
}

// getAccount retrieves a single account. If ?reveal=true is passed, it returns the decrypted secret
// and creates an audit log entry.
func (p *Plugin) getAccount(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	id := getID(r)
	if id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	reveal := r.URL.Query().Get("reveal") == "true"
	if reveal && scope.Team && !p.requireRole(r, "admin") {
		http.Error(w, "forbidden: revealing secret requires admin role", http.StatusForbidden)
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	meta, err := p.requireTwoFAItem(r.Context(), vs, scope, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	acc := metaToAccount(*meta)
	currentCode, rem, _ := vs.ComputeTOTP(r.Context(), scope.OrgID, id)

	resp := map[string]any{
		"id":                acc.ID,
		"created_at":        acc.CreatedAt,
		"updated_at":        acc.UpdatedAt,
		"org_id":            acc.OrgID,
		"name":              acc.Name,
		"issuer":            acc.Issuer,
		"account":           acc.Account,
		"algorithm":         acc.Algorithm,
		"digits":            acc.Digits,
		"period":            acc.Period,
		"tags":              acc.Tags,
		"notes":             acc.Notes,
		"pinned":            acc.Pinned,
		"current_code":      currentCode,
		"seconds_remaining": rem,
	}

	if reveal {
		sec, sErr := vs.GetSecret(r.Context(), scope.OrgID, id)
		if sErr != nil {
			http.Error(w, "failed to decrypt secret", http.StatusInternalServerError)
			return
		}
		resp["secret"] = extractSecretFromPayload(sec.Payload)
		p.logAudit(r, scope.OrgID, acc.ID, acc.Name, "view_secret", p.actor(r), r.RemoteAddr)
	}

	writeJSON(w, resp)
}

type createAccountIn struct {
	Name       string `json:"name"`
	Issuer     string `json:"issuer"`
	Account    string `json:"account"`
	Secret     string `json:"secret"`
	Algorithm  string `json:"algorithm"`
	Digits     int    `json:"digits"`
	Period     int    `json:"period"`
	Tags       string `json:"tags"`
	Notes      string `json:"notes"`
	Pinned     bool   `json:"pinned"`
	OTPAuthURL string `json:"otpauth_url"`
}

func (p *Plugin) createAccount(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}

	if scope.Team && !p.requireRole(r, "admin") {
		http.Error(w, "forbidden: creating account requires admin role", http.StatusForbidden)
		return
	}

	var in createAccountIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var parsedSecret string
	var acc TwoFAAccount

	if strings.TrimSpace(in.OTPAuthURL) != "" {
		parsedAcc, s, err := ParseOTPAuthURI(in.OTPAuthURL)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid otpauth url: %v", err), http.StatusBadRequest)
			return
		}
		acc = *parsedAcc
		parsedSecret = s
	}

	if in.Name != "" {
		acc.Name = in.Name
	}
	if in.Issuer != "" {
		acc.Issuer = in.Issuer
	}
	if in.Account != "" {
		acc.Account = in.Account
	}
	if in.Algorithm != "" {
		acc.Algorithm = in.Algorithm
	}
	if in.Digits > 0 {
		acc.Digits = in.Digits
	}
	if in.Period > 0 {
		acc.Period = in.Period
	}
	if in.Tags != "" {
		acc.Tags = in.Tags
	}
	if in.Notes != "" {
		acc.Notes = in.Notes
	}
	acc.Pinned = in.Pinned

	finalSecret := parsedSecret
	if in.Secret != "" {
		finalSecret = NormalizeSecret(in.Secret)
	}

	if finalSecret == "" {
		http.Error(w, "secret is required", http.StatusBadRequest)
		return
	}

	if _, err := DecodeBase32(finalSecret); err != nil {
		http.Error(w, "invalid base32 secret", http.StatusBadRequest)
		return
	}

	if acc.Name == "" {
		if acc.Issuer != "" {
			acc.Name = acc.Issuer
		} else {
			acc.Name = "Unnamed 2FA"
		}
	}
	if acc.Algorithm == "" {
		acc.Algorithm = "SHA1"
	}
	if acc.Digits == 0 {
		acc.Digits = 6
	}
	if acc.Period == 0 {
		acc.Period = 30
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	meta, err := vs.PutSecret(r.Context(), plugin.PutSecretRequest{
		OrgID:          scope.OrgID,
		Scope:          scope.Scope,
		OwnerUserID:    scope.OwnerID,
		CollectionSlug: "default",
		ItemType:       "twofa_totp",
		Name:           acc.Name,
		Tags:           splitTags(acc.Tags),
		Metadata:       buildTwoFAMetadata(&acc),
		Payload:        buildTwoFAPayload(&acc, finalSecret),
		ActorUserID:    p.actorUserID(r),
	})
	if err != nil {
		http.Error(w, "failed to save account", http.StatusInternalServerError)
		return
	}

	p.logAudit(r, scope.OrgID, meta.ID, meta.Name, "create_account", p.actor(r), r.RemoteAddr)

	currentCode, rem, _ := vs.ComputeTOTP(r.Context(), scope.OrgID, meta.ID)

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, metaToSummary(*meta, currentCode, rem))
}

type updateAccountIn struct {
	Name      *string `json:"name,omitempty"`
	Issuer    *string `json:"issuer,omitempty"`
	Account   *string `json:"account,omitempty"`
	Secret    string  `json:"secret,omitempty"`
	Algorithm *string `json:"algorithm,omitempty"`
	Digits    int     `json:"digits"`
	Period    int     `json:"period"`
	Tags      *string `json:"tags,omitempty"`
	Notes     *string `json:"notes,omitempty"`
	Pinned    *bool   `json:"pinned,omitempty"`
}

func (p *Plugin) updateAccount(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	id := getID(r)
	if id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if scope.Team && !p.requireRole(r, "admin") {
		http.Error(w, "forbidden: updating account requires admin role", http.StatusForbidden)
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	meta, err := p.requireTwoFAItem(r.Context(), vs, scope, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}
	acc := metaToAccount(*meta)
	prevIssuer, prevAccount := acc.Issuer, acc.Account
	prevAlgorithm, prevDigits, prevPeriod := acc.Algorithm, acc.Digits, acc.Period

	var in updateAccountIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		acc.Name = *in.Name
	}
	if in.Issuer != nil {
		acc.Issuer = *in.Issuer
	}
	if in.Account != nil {
		acc.Account = *in.Account
	}
	if in.Algorithm != nil && strings.TrimSpace(*in.Algorithm) != "" {
		acc.Algorithm = *in.Algorithm
	}
	if in.Digits >= 6 && in.Digits <= 8 {
		acc.Digits = in.Digits
	}
	if in.Period > 0 {
		acc.Period = in.Period
	}
	if in.Tags != nil {
		acc.Tags = *in.Tags
	}
	if in.Notes != nil {
		acc.Notes = *in.Notes
	}
	if in.Pinned != nil {
		acc.Pinned = *in.Pinned
	}

	paramsChanged := acc.Issuer != prevIssuer ||
		acc.Account != prevAccount ||
		acc.Algorithm != prevAlgorithm ||
		acc.Digits != prevDigits ||
		acc.Period != prevPeriod

	var payload map[string]any
	if in.Secret != "" {
		clean := NormalizeSecret(in.Secret)
		if _, err := DecodeBase32(clean); err != nil {
			http.Error(w, "invalid base32 secret", http.StatusBadRequest)
			return
		}
		payload = buildTwoFAPayload(&acc, clean)
	} else if paramsChanged {
		sec, sErr := vs.GetSecret(r.Context(), scope.OrgID, id)
		if sErr != nil {
			if errors.Is(sErr, plugin.ErrSecretNotFound) {
				http.Error(w, "account not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to load existing secret", http.StatusInternalServerError)
			return
		}
		existingSecret := extractSecretFromPayload(sec.Payload)
		if existingSecret == "" {
			http.Error(w, "failed to extract existing secret", http.StatusInternalServerError)
			return
		}
		payload = buildTwoFAPayload(&acc, existingSecret)
	}

	updated, err := vs.UpdateSecret(r.Context(), scope.OrgID, id, &acc.Name, buildTwoFAMetadata(&acc), payload, nil, "Updated 2FA account", p.actorUserID(r))
	if err != nil {
		http.Error(w, "failed to update account", http.StatusInternalServerError)
		return
	}

	p.logAudit(r, scope.OrgID, updated.ID, updated.Name, "update_account", p.actor(r), r.RemoteAddr)

	currentCode, rem, _ := vs.ComputeTOTP(r.Context(), scope.OrgID, updated.ID)
	writeJSON(w, metaToSummary(*updated, currentCode, rem))
}

func (p *Plugin) deleteAccount(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	id := getID(r)
	if id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if scope.Team && !p.requireRole(r, "admin") {
		http.Error(w, "forbidden: deleting account requires admin role", http.StatusForbidden)
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	meta, err := p.requireTwoFAItem(r.Context(), vs, scope, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	if err := vs.DeleteSecret(r.Context(), scope.OrgID, id); err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete account", http.StatusInternalServerError)
		return
	}

	p.logAudit(r, scope.OrgID, meta.ID, meta.Name, "delete_account", p.actor(r), r.RemoteAddr)
	writeJSON(w, map[string]bool{"deleted": true})
}

func (p *Plugin) togglePin(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	id := getID(r)
	if id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if scope.Team && !p.requireRole(r, "admin") {
		http.Error(w, "forbidden: pinning account requires admin role", http.StatusForbidden)
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	meta, err := p.requireTwoFAItem(r.Context(), vs, scope, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	pinned := !metaBool(meta.Metadata, "pinned")
	if _, err := vs.UpdateSecret(r.Context(), scope.OrgID, id, nil, map[string]any{"pinned": pinned}, nil, nil, "Toggled pin", p.actorUserID(r)); err != nil {
		http.Error(w, "failed to update pin", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"pinned": pinned})
}

func (p *Plugin) getCode(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	id := getID(r)
	if id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	meta, err := p.requireTwoFAItem(r.Context(), vs, scope, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	code, rem, err := vs.ComputeTOTP(r.Context(), scope.OrgID, id)
	if err != nil {
		http.Error(w, "failed to generate code", http.StatusInternalServerError)
		return
	}
	validUntil := time.Now().Add(time.Duration(rem) * time.Second)

	p.logAudit(r, scope.OrgID, meta.ID, meta.Name, "generate_code", p.actor(r), r.RemoteAddr)

	writeJSON(w, map[string]any{
		"id":                meta.ID,
		"code":              code,
		"seconds_remaining": rem,
		"valid_until":       validUntil.UTC().Format(time.RFC3339),
	})
}

type verifyIn struct {
	Code string `json:"code"`
}

func (p *Plugin) verifyCode(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	id := getID(r)
	if id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var in verifyIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	if _, err := p.requireTwoFAItem(r.Context(), vs, scope, id); err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	sec, err := vs.GetSecret(r.Context(), scope.OrgID, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to decrypt secret", http.StatusInternalServerError)
		return
	}

	acc := metaToAccount(sec.Meta)
	secret := extractSecretFromPayload(sec.Payload)
	valid := VerifyCode(secret, in.Code, time.Now(), acc.Period, acc.Digits, acc.Algorithm, 1)
	p.logAudit(r, scope.OrgID, acc.ID, acc.Name, "verify_code", p.actor(r), r.RemoteAddr)

	writeJSON(w, map[string]bool{"valid": valid})
}

func (p *Plugin) getQRCode(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	id := getID(r)
	if id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if scope.Team && !p.requireRole(r, "admin") {
		http.Error(w, "forbidden: retrieving QR code requires admin role", http.StatusForbidden)
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	if _, err := p.requireTwoFAItem(r.Context(), vs, scope, id); err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	sec, err := vs.GetSecret(r.Context(), scope.OrgID, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to decrypt secret", http.StatusInternalServerError)
		return
	}

	acc := metaToAccount(sec.Meta)
	secret := extractSecretFromPayload(sec.Payload)
	uri := BuildOTPAuthURI(&acc, secret)
	p.logAudit(r, scope.OrgID, acc.ID, acc.Name, "view_qr", p.actor(r), r.RemoteAddr)

	if r.URL.Query().Get("format") == "image" {
		png, err := GenerateQRCodePNG(uri, 256)
		if err != nil {
			http.Error(w, "failed to generate qr code", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
		return
	}

	dataURL, err := GenerateQRCodeDataURI(uri, 256)
	if err != nil {
		http.Error(w, "failed to generate qr data uri", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{
		"uri":         uri,
		"qr_data_url": dataURL,
	})
}

type importIn struct {
	URIs []string `json:"uris"`
	Tags string   `json:"tags"`
}

func (p *Plugin) importAccounts(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}

	if scope.Team && !p.requireRole(r, "admin") {
		http.Error(w, "forbidden: importing accounts requires admin role", http.StatusForbidden)
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	var in importIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	imported := 0
	errs := make([]string, 0)
	actorID := p.actorUserID(r)

	for idx, u := range in.URIs {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		acc, s, err := ParseOTPAuthURI(u)
		if err != nil {
			errs = append(errs, fmt.Sprintf("line %d: %v", idx+1, err))
			continue
		}

		if in.Tags != "" {
			if acc.Tags != "" {
				acc.Tags += ", " + in.Tags
			} else {
				acc.Tags = in.Tags
			}
		}

		if _, err := vs.PutSecret(r.Context(), plugin.PutSecretRequest{
			OrgID:          scope.OrgID,
			Scope:          scope.Scope,
			OwnerUserID:    scope.OwnerID,
			CollectionSlug: "default",
			ItemType:       "twofa_totp",
			Name:           acc.Name,
			Tags:           splitTags(acc.Tags),
			Metadata:       buildTwoFAMetadata(acc),
			Payload:        buildTwoFAPayload(acc, s),
			ActorUserID:    actorID,
		}); err != nil {
			errs = append(errs, fmt.Sprintf("line %d: vault error: %v", idx+1, err))
			continue
		}
		imported++
	}

	p.logAudit(r, scope.OrgID, 0, fmt.Sprintf("imported %d accounts", imported), "import_accounts", p.actor(r), r.RemoteAddr)

	writeJSON(w, map[string]any{
		"imported": imported,
		"errors":   errs,
	})
}

func (p *Plugin) exportAccounts(w http.ResponseWriter, r *http.Request) {
	scope, ok := p.requireScope(w, r)
	if !ok {
		return
	}

	if scope.Team && !p.requireRole(r, "admin") {
		http.Error(w, "forbidden: exporting accounts requires admin role", http.StatusForbidden)
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	items, err := vs.ListItems(r.Context(), scope.OrgID, plugin.VaultFilter{
		Scope:       scope.Scope,
		ItemType:    "twofa_totp",
		OwnerUserID: scope.OwnerID,
	})
	if err != nil {
		http.Error(w, "vault query error", http.StatusInternalServerError)
		return
	}

	type exportItem struct {
		Name      string `json:"name"`
		Issuer    string `json:"issuer"`
		Account   string `json:"account"`
		Algorithm string `json:"algorithm"`
		Digits    int    `json:"digits"`
		Period    int    `json:"period"`
		Tags      string `json:"tags"`
		Notes     string `json:"notes"`
		URI       string `json:"uri"`
	}

	out := make([]exportItem, 0, len(items))
	for _, item := range items {
		sec, err := vs.GetSecret(r.Context(), scope.OrgID, item.ID)
		if err != nil {
			continue
		}
		acc := metaToAccount(sec.Meta)
		secret := extractSecretFromPayload(sec.Payload)
		uri := BuildOTPAuthURI(&acc, secret)
		out = append(out, exportItem{
			Name:      acc.Name,
			Issuer:    acc.Issuer,
			Account:   acc.Account,
			Algorithm: acc.Algorithm,
			Digits:    acc.Digits,
			Period:    acc.Period,
			Tags:      acc.Tags,
			Notes:     acc.Notes,
			URI:       uri,
		})
	}

	p.logAudit(r, scope.OrgID, 0, fmt.Sprintf("exported %d accounts", len(out)), "export_accounts", p.actor(r), r.RemoteAddr)
	writeJSON(w, out)
}

func (p *Plugin) logAudit(r *http.Request, orgID uint, accountID uint, accountName string, action string, actor string, ip string) {
	if orgID == 0 || r == nil {
		return
	}
	if p.ctx != nil && p.ctx.Audit != nil {
		p.ctx.Audit(r, "twofa."+action, "twofa_account", accountID, map[string]any{
			"account_name": accountName,
			"actor":        actor,
			"ip":           ip,
			"org_id":       orgID,
		})
	}
}

func (p *Plugin) requireRole(r *http.Request, min string) bool {
	if p.host != nil && p.host.Session() != nil {
		return p.host.Session().RequireRole(r, min)
	}
	return false
}

func (p *Plugin) actorUserID(r *http.Request) uint {
	if p.host != nil && p.host.Session() != nil && r != nil {
		return p.host.Session().UserID(r)
	}
	return 0
}

func (p *Plugin) actor(r *http.Request) string {
	if uid := p.actorUserID(r); uid > 0 {
		return fmt.Sprintf("user:%d", uid)
	}
	return "dashboard"
}

func (p *Plugin) orgID(r *http.Request) uint {
	if p.host != nil && p.host.Session() != nil {
		return p.host.Session().OrgID(r)
	}
	return 0
}

func getID(r *http.Request) uint {
	idStr := r.PathValue("id")
	if idStr == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		for i, p := range parts {
			if p == "accounts" && i+1 < len(parts) {
				idStr = parts[i+1]
				break
			}
		}
	}
	id, _ := strconv.ParseUint(idStr, 10, 32)
	return uint(id)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
