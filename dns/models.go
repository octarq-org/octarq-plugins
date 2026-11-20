package dns

import (
	"time"

	"github.com/octarq-org/octarq/server/pkg/hostlist"
)

type ProviderAccount struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	OrgID          uint      `gorm:"column:owner_id;index;default:1" json:"-"`
	Name           string    `gorm:"size:255" json:"name"`
	Type           string    `gorm:"size:32" json:"type"`
	VaultItemID    uint      `gorm:"index" json:"vaultItemId,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	HasCredentials bool      `gorm:"-" json:"hasCredentials"`
}

type Domain struct {
	ID                uint              `gorm:"primaryKey" json:"id"`
	OrgID             uint              `gorm:"column:owner_id;index;default:1" json:"-"`
	Name              string            `gorm:"uniqueIndex;size:255" json:"name"`
	ProviderAccountID uint              `gorm:"index" json:"providerAccountId"`
	ZoneID            string            `gorm:"size:64" json:"zoneId"`
	Note              string            `gorm:"type:text" json:"note"`
	ForMail           bool              `json:"forMail"`
	ForLink           bool              `json:"forLink"`
	LinkHosts         hostlist.HostList `gorm:"type:text" json:"linkHosts"`
	MailHosts         hostlist.HostList `gorm:"type:text" json:"mailHosts"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

func (d Domain) EffectiveLinkHosts() []string { return d.LinkHosts.Enabled() }

func (d Domain) EffectiveMailHosts() []string { return d.MailHosts.Enabled() }

func NormalizeHost(host string) string {
	return hostlist.NormalizeHost(host)
}

type DDNSToken struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	OrgID      uint       `gorm:"column:owner_id;index;default:1" json:"-"`
	DomainID   uint       `gorm:"index" json:"domainId"`
	RecordName string     `gorm:"size:255" json:"recordName"`
	RecordType string     `gorm:"size:8" json:"recordType"`
	TokenHash  string     `gorm:"uniqueIndex;size:64" json:"-"`
	Label      string     `gorm:"size:255" json:"label"`
	LastIP     string     `gorm:"size:64" json:"lastIp"`
	LastSeenAt *time.Time `json:"lastSeenAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func (DDNSToken) TableName() string { return "dns_ddns_tokens" }
