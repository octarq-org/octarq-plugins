package twofa

import (
	"time"
)

// TwoFAAccount is the in-memory representation of a 2FA/TOTP account backed
// by core VaultService (`item_type="twofa_totp"`). Secrets are never stored
// in a plugin-owned table.
type TwoFAAccount struct {
	ID        uint      `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	OrgID     uint      `json:"org_id"`
	Name      string    `json:"name"`
	Issuer    string    `json:"issuer"`
	Account   string    `json:"account"`
	Algorithm string    `json:"algorithm"`
	Digits    int       `json:"digits"`
	Period    int       `json:"period"`
	Tags      string    `json:"tags"`
	Notes     string    `json:"notes"`
	Pinned    bool      `json:"pinned"`
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
