package twofa

import (
	"time"

	"gorm.io/gorm"
)

// TwoFAAccount represents a registered 2FA/TOTP credential item in the vault.
// Sensitive secrets are stored encrypted with AES-256-GCM and never exposed
// in standard list APIs.
type TwoFAAccount struct {
	gorm.Model
	OrgID           uint   `gorm:"index;not null" json:"org_id"`
	Name            string `gorm:"size:255;not null;index" json:"name"`
	Issuer          string `gorm:"size:255" json:"issuer"`
	Account         string `gorm:"size:255" json:"account"`
	EncryptedSecret string `gorm:"type:text;not null" json:"-"`
	Algorithm       string `gorm:"size:32;default:'SHA1'" json:"algorithm"`
	Digits          int    `gorm:"default:6" json:"digits"`
	Period          int    `gorm:"default:30" json:"period"`
	Tags            string `gorm:"size:512" json:"tags"`
	Notes           string `gorm:"type:text" json:"notes"`
	Pinned          bool   `gorm:"default:false;index" json:"pinned"`
}

// TwoFAAuditLog records every security-sensitive interaction with the 2FA vault,
// such as code generations, secret reveals, exports, and MCP agent queries.
type TwoFAAuditLog struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
	OrgID       uint      `gorm:"index;not null" json:"org_id"`
	AccountID   uint      `gorm:"index" json:"account_id"`
	AccountName string    `gorm:"size:255" json:"account_name"`
	Action      string    `gorm:"size:64;not null;index" json:"action"`
	Actor       string    `gorm:"size:255" json:"actor"`
	IP          string    `gorm:"size:64" json:"ip"`
}

// AccountSummary is a safe projection of TwoFAAccount containing live TOTP codes
// for frontend dashboard rendering, omitting secret data.
type AccountSummary struct {
	ID               uint      `json:"id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	OrgID            uint      `json:"org_id"`
	Name             string    `json:"name"`
	Issuer           string    `json:"issuer"`
	Account          string    `json:"account"`
	Algorithm        string    `json:"algorithm"`
	Digits           int       `json:"digits"`
	Period           int       `json:"period"`
	Tags             string    `json:"tags"`
	Notes            string    `json:"notes"`
	Pinned           bool      `json:"pinned"`
	CurrentCode      string    `json:"current_code,omitempty"`
	SecondsRemaining int       `json:"seconds_remaining"`
}
