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
	"gorm.io/gorm/clause"
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

func metaToSummary(item plugin.VaultItemMeta, code string, rem int, mine, shared bool) AccountSummary {
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
		Mine:             mine,
		Shared:           shared,
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

var errShareStoreUnavailable = errors.New("share store unavailable")

type requestScope struct {
	OrgID  uint
	UserID uint
}

func (p *Plugin) requireScope(w http.ResponseWriter, r *http.Request) (requestScope, bool) {
	orgID := p.orgID(r)
	userID := p.actorUserID(r)
	if orgID == 0 || userID == 0 {
		http.Error(w, "unauthorized workspace", http.StatusUnauthorized)
		return requestScope{}, false
	}
	return requestScope{OrgID: orgID, UserID: userID}, true
}

func (p *Plugin) requireShareStore(w http.ResponseWriter) bool {
	if p.db == nil {
		http.Error(w, errShareStoreUnavailable.Error(), http.StatusInternalServerError)
		return false
	}
	return true
}

func (p *Plugin) loadTwoFAItem(ctx context.Context, vs plugin.VaultService, orgID, id uint) (*plugin.VaultItemMeta, error) {
	meta, err := vs.GetMeta(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if meta == nil || meta.OrgID != orgID || meta.ItemType != "twofa_totp" || meta.Scope != plugin.VaultScopeUser {
		return nil, plugin.ErrSecretNotFound
	}
	return meta, nil
}

func (p *Plugin) requireOwnedItem(ctx context.Context, vs plugin.VaultService, sc requestScope, id uint) (*plugin.VaultItemMeta, error) {
	meta, err := p.loadTwoFAItem(ctx, vs, sc.OrgID, id)
	if err != nil {
		return nil, err
	}
	if meta.OwnerUserID != sc.UserID {
		return nil, plugin.ErrSecretNotFound
	}
	return meta, nil
}

func (p *Plugin) requireReadableItem(ctx context.Context, vs plugin.VaultService, sc requestScope, id uint) (*plugin.VaultItemMeta, error) {
	meta, err := p.loadTwoFAItem(ctx, vs, sc.OrgID, id)
	if err != nil {
		return nil, err
	}
	if meta.OwnerUserID == sc.UserID {
		return meta, nil
	}
	shared, err := p.isShared(ctx, sc.OrgID, id)
	if err != nil {
		return nil, err
	}
	if !shared {
		return nil, plugin.ErrSecretNotFound
	}
	return meta, nil
}

func (p *Plugin) isShared(ctx context.Context, orgID, itemID uint) (bool, error) {
	if p.db == nil {
		return false, errShareStoreUnavailable
	}
	var count int64
	if err := p.db.WithContext(ctx).Model(&TwoFAShare{}).
		Where("org_id = ? AND item_id = ?", orgID, itemID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (p *Plugin) deleteShare(ctx context.Context, orgID, itemID uint) error {
	if p.db == nil {
		return errShareStoreUnavailable
	}
	return p.db.WithContext(ctx).Where("org_id = ? AND item_id = ?", orgID, itemID).Delete(&TwoFAShare{}).Error
}

func (p *Plugin) resolveSharedItem(ctx context.Context, vs plugin.VaultService, orgID uint, accountName string) (*plugin.VaultItemMeta, error) {
	if p.db == nil {
		return nil, errShareStoreUnavailable
	}
	var shares []TwoFAShare
	if err := p.db.WithContext(ctx).Where("org_id = ?", orgID).Find(&shares).Error; err != nil {
		return nil, err
	}
	name := strings.TrimSpace(accountName)
	for _, sh := range shares {
		meta, err := vs.GetMeta(ctx, orgID, sh.ItemID)
		if err != nil {
			if errors.Is(err, plugin.ErrSecretNotFound) {
				continue
			}
			return nil, err
		}
		if meta == nil || meta.OrgID != orgID || meta.ItemType != "twofa_totp" || meta.Scope != plugin.VaultScopeUser {
			continue
		}
		acc := metaToAccount(*meta)
		if strings.EqualFold(acc.Name, name) || strings.EqualFold(acc.Issuer, name) {
			return meta, nil
		}
	}
	return nil, fmt.Errorf("shared account %q not found in workspace %d", accountName, orgID)
}

func (p *Plugin) listAccounts(w http.ResponseWriter, r *http.Request) {
	sc, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	if !p.requireShareStore(w) {
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	own, err := vs.ListItems(r.Context(), sc.OrgID, plugin.VaultFilter{
		Scope:       plugin.VaultScopeUser,
		ItemType:    "twofa_totp",
		OwnerUserID: sc.UserID,
	})
	if err != nil {
		http.Error(w, "failed to query accounts", http.StatusInternalServerError)
		return
	}

	var shares []TwoFAShare
	if err := p.db.WithContext(r.Context()).Where("org_id = ?", sc.OrgID).Find(&shares).Error; err != nil {
		http.Error(w, "failed to query shared accounts", http.StatusInternalServerError)
		return
	}
	sharedSet := make(map[uint]bool, len(shares))
	for _, sh := range shares {
		sharedSet[sh.ItemID] = true
	}

	summaries := make([]AccountSummary, 0, len(own)+len(shares))
	for _, item := range own {
		code, rem, cErr := vs.ComputeTOTP(r.Context(), sc.OrgID, item.ID)
		if cErr != nil {
			code = ""
			rem = 0
		}
		summaries = append(summaries, metaToSummary(item, code, rem, true, sharedSet[item.ID]))
	}

	for _, sh := range shares {
		meta, mErr := vs.GetMeta(r.Context(), sc.OrgID, sh.ItemID)
		if mErr != nil {
			if errors.Is(mErr, plugin.ErrSecretNotFound) {
				_ = p.deleteShare(r.Context(), sc.OrgID, sh.ItemID)
				continue
			}
			http.Error(w, "failed to load shared accounts", http.StatusInternalServerError)
			return
		}
		if meta == nil || meta.OrgID != sc.OrgID || meta.ItemType != "twofa_totp" || meta.Scope != plugin.VaultScopeUser {
			_ = p.deleteShare(r.Context(), sc.OrgID, sh.ItemID)
			continue
		}
		if meta.OwnerUserID == sc.UserID {
			continue
		}
		code, rem, cErr := vs.ComputeTOTP(r.Context(), sc.OrgID, meta.ID)
		if cErr != nil {
			code = ""
			rem = 0
		}
		summaries = append(summaries, metaToSummary(*meta, code, rem, false, true))
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

func (p *Plugin) getAccount(w http.ResponseWriter, r *http.Request) {
	sc, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	if !p.requireShareStore(w) {
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

	meta, err := p.requireOwnedItem(r.Context(), vs, sc, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	shared, err := p.isShared(r.Context(), sc.OrgID, id)
	if err != nil {
		http.Error(w, "failed to load sharing state", http.StatusInternalServerError)
		return
	}

	acc := metaToAccount(*meta)
	currentCode, rem, _ := vs.ComputeTOTP(r.Context(), sc.OrgID, id)

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
		"mine":              true,
		"shared":            shared,
	}

	if r.URL.Query().Get("reveal") == "true" {
		sec, sErr := vs.GetSecret(r.Context(), sc.OrgID, id)
		if sErr != nil {
			http.Error(w, "failed to decrypt secret", http.StatusInternalServerError)
			return
		}
		resp["secret"] = extractSecretFromPayload(sec.Payload)
		p.logAudit(r, sc.OrgID, acc.ID, acc.Name, "view_secret", p.actor(r), r.RemoteAddr)
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
	sc, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	if !p.requireShareStore(w) {
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
		OrgID:          sc.OrgID,
		Scope:          plugin.VaultScopeUser,
		OwnerUserID:    sc.UserID,
		CollectionSlug: "default",
		ItemType:       "twofa_totp",
		Name:           acc.Name,
		Tags:           splitTags(acc.Tags),
		Metadata:       buildTwoFAMetadata(&acc),
		Payload:        buildTwoFAPayload(&acc, finalSecret),
		ActorUserID:    sc.UserID,
	})
	if err != nil {
		http.Error(w, "failed to save account", http.StatusInternalServerError)
		return
	}

	p.logAudit(r, sc.OrgID, meta.ID, meta.Name, "create_account", p.actor(r), r.RemoteAddr)

	currentCode, rem, _ := vs.ComputeTOTP(r.Context(), sc.OrgID, meta.ID)

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, metaToSummary(*meta, currentCode, rem, true, false))
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
	sc, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	if !p.requireShareStore(w) {
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

	meta, err := p.requireOwnedItem(r.Context(), vs, sc, id)
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
		sec, sErr := vs.GetSecret(r.Context(), sc.OrgID, id)
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

	updated, err := vs.UpdateSecret(r.Context(), sc.OrgID, id, &acc.Name, buildTwoFAMetadata(&acc), payload, nil, "Updated 2FA account", sc.UserID)
	if err != nil {
		http.Error(w, "failed to update account", http.StatusInternalServerError)
		return
	}

	p.logAudit(r, sc.OrgID, updated.ID, updated.Name, "update_account", p.actor(r), r.RemoteAddr)

	shared, err := p.isShared(r.Context(), sc.OrgID, id)
	if err != nil {
		http.Error(w, "failed to load sharing state", http.StatusInternalServerError)
		return
	}

	currentCode, rem, _ := vs.ComputeTOTP(r.Context(), sc.OrgID, updated.ID)
	writeJSON(w, metaToSummary(*updated, currentCode, rem, true, shared))
}

func (p *Plugin) deleteAccount(w http.ResponseWriter, r *http.Request) {
	sc, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	if !p.requireShareStore(w) {
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

	meta, err := p.requireOwnedItem(r.Context(), vs, sc, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	if err := vs.DeleteSecret(r.Context(), sc.OrgID, id); err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete account", http.StatusInternalServerError)
		return
	}

	if err := p.deleteShare(r.Context(), sc.OrgID, id); err != nil {
		http.Error(w, "failed to delete share", http.StatusInternalServerError)
		return
	}

	p.logAudit(r, sc.OrgID, meta.ID, meta.Name, "delete_account", p.actor(r), r.RemoteAddr)
	writeJSON(w, map[string]bool{"deleted": true})
}

func (p *Plugin) togglePin(w http.ResponseWriter, r *http.Request) {
	sc, ok := p.requireScope(w, r)
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

	meta, err := p.requireOwnedItem(r.Context(), vs, sc, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	pinned := !metaBool(meta.Metadata, "pinned")
	if _, err := vs.UpdateSecret(r.Context(), sc.OrgID, id, nil, map[string]any{"pinned": pinned}, nil, nil, "Toggled pin", sc.UserID); err != nil {
		http.Error(w, "failed to update pin", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"pinned": pinned})
}

type shareIn struct {
	Shared bool `json:"shared"`
}

func (p *Plugin) setShare(w http.ResponseWriter, r *http.Request) {
	sc, ok := p.requireScope(w, r)
	if !ok {
		return
	}
	if !p.requireShareStore(w) {
		return
	}
	id := getID(r)
	if id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var in shareIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	meta, err := p.requireOwnedItem(r.Context(), vs, sc, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	if in.Shared {
		row := TwoFAShare{OrgID: sc.OrgID, ItemID: id, SharedBy: sc.UserID, CreatedAt: time.Now().UTC()}
		if err := p.db.WithContext(r.Context()).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&row).Error; err != nil {
			http.Error(w, "failed to share account", http.StatusInternalServerError)
			return
		}
		p.logAudit(r, sc.OrgID, meta.ID, meta.Name, "share_account", p.actor(r), r.RemoteAddr)
	} else {
		if err := p.deleteShare(r.Context(), sc.OrgID, id); err != nil {
			http.Error(w, "failed to unshare account", http.StatusInternalServerError)
			return
		}
		p.logAudit(r, sc.OrgID, meta.ID, meta.Name, "unshare_account", p.actor(r), r.RemoteAddr)
	}

	writeJSON(w, map[string]bool{"shared": in.Shared})
}

func (p *Plugin) getCode(w http.ResponseWriter, r *http.Request) {
	sc, ok := p.requireScope(w, r)
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

	meta, err := p.requireReadableItem(r.Context(), vs, sc, id)
	if err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	code, rem, err := vs.ComputeTOTP(r.Context(), sc.OrgID, id)
	if err != nil {
		http.Error(w, "failed to generate code", http.StatusInternalServerError)
		return
	}
	validUntil := time.Now().Add(time.Duration(rem) * time.Second)

	p.logAudit(r, sc.OrgID, meta.ID, meta.Name, "generate_code", p.actor(r), r.RemoteAddr)

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
	sc, ok := p.requireScope(w, r)
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

	if _, err := p.requireReadableItem(r.Context(), vs, sc, id); err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	sec, err := vs.GetSecret(r.Context(), sc.OrgID, id)
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
	p.logAudit(r, sc.OrgID, acc.ID, acc.Name, "verify_code", p.actor(r), r.RemoteAddr)

	writeJSON(w, map[string]bool{"valid": valid})
}

func (p *Plugin) getQRCode(w http.ResponseWriter, r *http.Request) {
	sc, ok := p.requireScope(w, r)
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

	if _, err := p.requireOwnedItem(r.Context(), vs, sc, id); err != nil {
		if errors.Is(err, plugin.ErrSecretNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	sec, err := vs.GetSecret(r.Context(), sc.OrgID, id)
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
	p.logAudit(r, sc.OrgID, acc.ID, acc.Name, "view_qr", p.actor(r), r.RemoteAddr)

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
	sc, ok := p.requireScope(w, r)
	if !ok {
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
			OrgID:          sc.OrgID,
			Scope:          plugin.VaultScopeUser,
			OwnerUserID:    sc.UserID,
			CollectionSlug: "default",
			ItemType:       "twofa_totp",
			Name:           acc.Name,
			Tags:           splitTags(acc.Tags),
			Metadata:       buildTwoFAMetadata(acc),
			Payload:        buildTwoFAPayload(acc, s),
			ActorUserID:    sc.UserID,
		}); err != nil {
			errs = append(errs, fmt.Sprintf("line %d: vault error: %v", idx+1, err))
			continue
		}
		imported++
	}

	p.logAudit(r, sc.OrgID, 0, fmt.Sprintf("imported %d accounts", imported), "import_accounts", p.actor(r), r.RemoteAddr)

	writeJSON(w, map[string]any{
		"imported": imported,
		"errors":   errs,
	})
}

func (p *Plugin) exportAccounts(w http.ResponseWriter, r *http.Request) {
	sc, ok := p.requireScope(w, r)
	if !ok {
		return
	}

	vs, err := p.vault()
	if err != nil {
		http.Error(w, "credential vault unavailable", http.StatusInternalServerError)
		return
	}

	items, err := vs.ListItems(r.Context(), sc.OrgID, plugin.VaultFilter{
		Scope:       plugin.VaultScopeUser,
		ItemType:    "twofa_totp",
		OwnerUserID: sc.UserID,
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
		sec, err := vs.GetSecret(r.Context(), sc.OrgID, item.ID)
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

	p.logAudit(r, sc.OrgID, 0, fmt.Sprintf("exported %d accounts", len(out)), "export_accounts", p.actor(r), r.RemoteAddr)
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
