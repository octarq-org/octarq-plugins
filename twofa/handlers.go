package twofa

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// listAccounts returns safe summaries for all 2FA accounts belonging to the active org,
// including real-time TOTP codes and time remaining.
func (p *Plugin) listAccounts(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	if orgID == 0 {
		http.Error(w, "unauthorized workspace", http.StatusUnauthorized)
		return
	}

	var accounts []TwoFAAccount
	if p.ctx != nil && p.ctx.DB != nil {
		p.ctx.DB.WithContext(r.Context()).
			Where("org_id = ?", orgID).
			Order("pinned DESC, name ASC, id ASC").
			Find(&accounts)
	}

	now := time.Now()
	summaries := make([]AccountSummary, 0, len(accounts))
	for _, acc := range accounts {
		var currentCode string
		var rem int
		if secret, err := DecryptSecret(acc.EncryptedSecret, p.key); err == nil {
			if code, err := GenerateCode(secret, now, acc.Period, acc.Digits, acc.Algorithm); err == nil {
				currentCode = code
				rem = SecondsRemaining(now, acc.Period)
			}
		}

		summaries = append(summaries, AccountSummary{
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
			CurrentCode:      currentCode,
			SecondsRemaining: rem,
		})
	}

	writeJSON(w, summaries)
}

// getAccount retrieves a single account. If ?reveal=true is passed, it returns the decrypted secret
// and creates an audit log entry.
func (p *Plugin) getAccount(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	id := getID(r)
	if orgID == 0 || id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var acc TwoFAAccount
	if err := p.ctx.DB.WithContext(r.Context()).Where("id = ? AND org_id = ?", id, orgID).First(&acc).Error; err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	secret, err := DecryptSecret(acc.EncryptedSecret, p.key)
	if err != nil {
		http.Error(w, "failed to decrypt secret", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	currentCode, _ := GenerateCode(secret, now, acc.Period, acc.Digits, acc.Algorithm)
	rem := SecondsRemaining(now, acc.Period)

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

	if r.URL.Query().Get("reveal") == "true" {
		resp["secret"] = secret
		p.logAudit(r.Context(), orgID, acc.ID, acc.Name, "view_secret", p.actor(r), r.RemoteAddr)
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
	orgID := p.orgID(r)
	if orgID == 0 {
		http.Error(w, "unauthorized workspace", http.StatusUnauthorized)
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

	// Request parameters override or provide manual data
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

	enc, err := EncryptSecret(finalSecret, p.key)
	if err != nil {
		http.Error(w, "encryption failed", http.StatusInternalServerError)
		return
	}

	acc.OrgID = orgID
	acc.EncryptedSecret = enc

	if err := p.ctx.DB.WithContext(r.Context()).Create(&acc).Error; err != nil {
		http.Error(w, "failed to save account", http.StatusInternalServerError)
		return
	}

	p.logAudit(r.Context(), orgID, acc.ID, acc.Name, "create_account", p.actor(r), r.RemoteAddr)

	now := time.Now()
	currentCode, _ := GenerateCode(finalSecret, now, acc.Period, acc.Digits, acc.Algorithm)
	rem := SecondsRemaining(now, acc.Period)

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, AccountSummary{
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
		CurrentCode:      currentCode,
		SecondsRemaining: rem,
	})
}

type updateAccountIn struct {
	Name      string `json:"name"`
	Issuer    string `json:"issuer"`
	Account   string `json:"account"`
	Secret    string `json:"secret,omitempty"`
	Algorithm string `json:"algorithm"`
	Digits    int    `json:"digits"`
	Period    int    `json:"period"`
	Tags      string `json:"tags"`
	Notes     string `json:"notes"`
	Pinned    *bool  `json:"pinned,omitempty"`
}

func (p *Plugin) updateAccount(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	id := getID(r)
	if orgID == 0 || id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var acc TwoFAAccount
	if err := p.ctx.DB.WithContext(r.Context()).Where("id = ? AND org_id = ?", id, orgID).First(&acc).Error; err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	var in updateAccountIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
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
	if in.Digits >= 6 && in.Digits <= 8 {
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
	if in.Pinned != nil {
		acc.Pinned = *in.Pinned
	}

	if in.Secret != "" {
		clean := NormalizeSecret(in.Secret)
		if _, err := DecodeBase32(clean); err != nil {
			http.Error(w, "invalid base32 secret", http.StatusBadRequest)
			return
		}
		enc, err := EncryptSecret(clean, p.key)
		if err != nil {
			http.Error(w, "encryption failed", http.StatusInternalServerError)
			return
		}
		acc.EncryptedSecret = enc
	}

	if err := p.ctx.DB.WithContext(r.Context()).Save(&acc).Error; err != nil {
		http.Error(w, "failed to update account", http.StatusInternalServerError)
		return
	}

	p.logAudit(r.Context(), orgID, acc.ID, acc.Name, "update_account", p.actor(r), r.RemoteAddr)

	secret, _ := DecryptSecret(acc.EncryptedSecret, p.key)
	now := time.Now()
	currentCode, _ := GenerateCode(secret, now, acc.Period, acc.Digits, acc.Algorithm)
	rem := SecondsRemaining(now, acc.Period)

	writeJSON(w, AccountSummary{
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
		CurrentCode:      currentCode,
		SecondsRemaining: rem,
	})
}

func (p *Plugin) deleteAccount(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	id := getID(r)
	if orgID == 0 || id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var acc TwoFAAccount
	if err := p.ctx.DB.WithContext(r.Context()).Where("id = ? AND org_id = ?", id, orgID).First(&acc).Error; err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	if err := p.ctx.DB.WithContext(r.Context()).Delete(&acc).Error; err != nil {
		http.Error(w, "failed to delete account", http.StatusInternalServerError)
		return
	}

	p.logAudit(r.Context(), orgID, acc.ID, acc.Name, "delete_account", p.actor(r), r.RemoteAddr)
	writeJSON(w, map[string]bool{"deleted": true})
}

func (p *Plugin) togglePin(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	id := getID(r)
	if orgID == 0 || id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var acc TwoFAAccount
	if err := p.ctx.DB.WithContext(r.Context()).Where("id = ? AND org_id = ?", id, orgID).First(&acc).Error; err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	acc.Pinned = !acc.Pinned
	_ = p.ctx.DB.WithContext(r.Context()).Save(&acc)
	writeJSON(w, map[string]bool{"pinned": acc.Pinned})
}

func (p *Plugin) getCode(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	id := getID(r)
	if orgID == 0 || id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var acc TwoFAAccount
	if err := p.ctx.DB.WithContext(r.Context()).Where("id = ? AND org_id = ?", id, orgID).First(&acc).Error; err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	secret, err := DecryptSecret(acc.EncryptedSecret, p.key)
	if err != nil {
		http.Error(w, "failed to decrypt secret", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	code, err := GenerateCode(secret, now, acc.Period, acc.Digits, acc.Algorithm)
	if err != nil {
		http.Error(w, "failed to generate code", http.StatusInternalServerError)
		return
	}
	rem := SecondsRemaining(now, acc.Period)
	validUntil := now.Add(time.Duration(rem) * time.Second)

	p.logAudit(r.Context(), orgID, acc.ID, acc.Name, "generate_code", p.actor(r), r.RemoteAddr)

	writeJSON(w, map[string]any{
		"id":                acc.ID,
		"code":              code,
		"seconds_remaining": rem,
		"valid_until":       validUntil.UTC().Format(time.RFC3339),
	})
}

type verifyIn struct {
	Code string `json:"code"`
}

func (p *Plugin) verifyCode(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	id := getID(r)
	if orgID == 0 || id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var in verifyIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}

	var acc TwoFAAccount
	if err := p.ctx.DB.WithContext(r.Context()).Where("id = ? AND org_id = ?", id, orgID).First(&acc).Error; err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	secret, err := DecryptSecret(acc.EncryptedSecret, p.key)
	if err != nil {
		http.Error(w, "failed to decrypt secret", http.StatusInternalServerError)
		return
	}

	valid := VerifyCode(secret, in.Code, time.Now(), acc.Period, acc.Digits, acc.Algorithm, 1)
	p.logAudit(r.Context(), orgID, acc.ID, acc.Name, "verify_code", p.actor(r), r.RemoteAddr)

	writeJSON(w, map[string]bool{"valid": valid})
}

func (p *Plugin) getQRCode(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	id := getID(r)
	if orgID == 0 || id == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var acc TwoFAAccount
	if err := p.ctx.DB.WithContext(r.Context()).Where("id = ? AND org_id = ?", id, orgID).First(&acc).Error; err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	secret, err := DecryptSecret(acc.EncryptedSecret, p.key)
	if err != nil {
		http.Error(w, "failed to decrypt secret", http.StatusInternalServerError)
		return
	}

	uri := BuildOTPAuthURI(&acc, secret)
	p.logAudit(r.Context(), orgID, acc.ID, acc.Name, "view_qr", p.actor(r), r.RemoteAddr)

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
	orgID := p.orgID(r)
	if orgID == 0 {
		http.Error(w, "unauthorized workspace", http.StatusUnauthorized)
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

		enc, err := EncryptSecret(s, p.key)
		if err != nil {
			errs = append(errs, fmt.Sprintf("line %d: encryption error", idx+1))
			continue
		}

		acc.OrgID = orgID
		acc.EncryptedSecret = enc
		if in.Tags != "" {
			if acc.Tags != "" {
				acc.Tags += ", " + in.Tags
			} else {
				acc.Tags = in.Tags
			}
		}

		if err := p.ctx.DB.WithContext(r.Context()).Create(acc).Error; err != nil {
			errs = append(errs, fmt.Sprintf("line %d: db error: %v", idx+1, err))
			continue
		}
		imported++
	}

	p.logAudit(r.Context(), orgID, 0, fmt.Sprintf("imported %d accounts", imported), "import_accounts", p.actor(r), r.RemoteAddr)

	writeJSON(w, map[string]any{
		"imported": imported,
		"errors":   errs,
	})
}

func (p *Plugin) exportAccounts(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	if orgID == 0 {
		http.Error(w, "unauthorized workspace", http.StatusUnauthorized)
		return
	}

	var accounts []TwoFAAccount
	if err := p.ctx.DB.WithContext(r.Context()).Where("org_id = ?", orgID).Find(&accounts).Error; err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
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

	out := make([]exportItem, 0, len(accounts))
	for _, acc := range accounts {
		secret, err := DecryptSecret(acc.EncryptedSecret, p.key)
		if err != nil {
			continue
		}
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

	p.logAudit(r.Context(), orgID, 0, fmt.Sprintf("exported %d accounts", len(out)), "export_accounts", p.actor(r), r.RemoteAddr)
	writeJSON(w, out)
}

func (p *Plugin) listAuditLogs(w http.ResponseWriter, r *http.Request) {
	orgID := p.orgID(r)
	if orgID == 0 {
		http.Error(w, "unauthorized workspace", http.StatusUnauthorized)
		return
	}

	var logs []TwoFAAuditLog
	if p.ctx != nil && p.ctx.DB != nil {
		p.ctx.DB.WithContext(r.Context()).
			Where("org_id = ?", orgID).
			Order("created_at DESC").
			Limit(50).
			Find(&logs)
	}

	writeJSON(w, logs)
}

func (p *Plugin) logAudit(ctx context.Context, orgID uint, accountID uint, accountName string, action string, actor string, ip string) {
	if p.ctx == nil || p.ctx.DB == nil || orgID == 0 {
		return
	}
	entry := TwoFAAuditLog{
		OrgID:       orgID,
		AccountID:   accountID,
		AccountName: accountName,
		Action:      action,
		Actor:       actor,
		IP:          ip,
	}
	_ = p.ctx.DB.WithContext(ctx).Create(&entry).Error
}

func (p *Plugin) actor(r *http.Request) string {
	if p.host != nil && p.host.Session() != nil {
		uid := p.host.Session().UserID(r)
		if uid > 0 {
			return fmt.Sprintf("user:%d", uid)
		}
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
