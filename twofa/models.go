package twofa

import (
	"time"
)

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
	Mine             bool      `json:"mine"`
	Shared           bool      `json:"shared"`
}

type TwoFAShare struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	OrgID     uint      `gorm:"index;uniqueIndex:idx_twofa_share_org_item;not null" json:"org_id"`
	ItemID    uint      `gorm:"uniqueIndex:idx_twofa_share_org_item;not null" json:"item_id"`
	SharedBy  uint      `json:"shared_by"`
	CreatedAt time.Time `json:"created_at"`
}

func (TwoFAShare) TableName() string { return "twofa_shares" }
