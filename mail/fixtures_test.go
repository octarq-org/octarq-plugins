package mail

import (
	"time"

	"github.com/octarq-org/octarq/server/pkg/hostlist"
)

type testOrg struct {
	ID           uint   `gorm:"primaryKey"`
	Name         string `gorm:"size:255"`
	Slug         string `gorm:"uniqueIndex;size:255"`
	InboundToken string `gorm:"size:255"`
}

func (testOrg) TableName() string { return "orgs" }

type testSetting struct {
	Key   string `gorm:"primaryKey;size:255"`
	Value string `gorm:"type:text"`
}

func (testSetting) TableName() string { return "settings" }

type testAuditLog struct {
	ID         uint      `gorm:"primaryKey"`
	OrgID      uint      `gorm:"index"`
	ActorID    uint      `gorm:"index"`
	Action     string    `gorm:"size:64;index"`
	TargetType string    `gorm:"size:64"`
	TargetID   uint      `gorm:"index"`
	Meta       string    `gorm:"type:text"`
	IP         string    `gorm:"size:45"`
	CreatedAt  time.Time `gorm:"index"`
}

func (testAuditLog) TableName() string { return "audit_logs" }

type testNotificationChannel struct {
	ID        uint   `gorm:"primaryKey"`
	OrgID     uint   `gorm:"column:owner_id;index"`
	Name      string `gorm:"size:255"`
	Type      string `gorm:"size:64"`
	Config    string `gorm:"type:text"`
	Enabled   bool   `gorm:"default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (testNotificationChannel) TableName() string { return "notification_channels" }

type testDomain struct {
	ID                uint   `gorm:"primaryKey"`
	OrgID             uint   `gorm:"column:owner_id;index;default:1"`
	Name              string `gorm:"uniqueIndex;size:255"`
	ProviderAccountID uint   `gorm:"index"`
	ZoneID            string `gorm:"size:64"`
	Note              string `gorm:"type:text"`
	ForMail           bool
	ForLink           bool
	LinkHosts         hostlist.HostList `gorm:"type:text"`
	MailHosts         hostlist.HostList `gorm:"type:text"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (testDomain) TableName() string { return "domains" }

func (d testDomain) EffectiveMailHosts() []string { return d.MailHosts.Enabled() }

func testAllModels() []any {
	return []any{
		&testOrg{},
		&testSetting{},
		&testAuditLog{},
		&testNotificationChannel{},
		&testDomain{},
		&Mailbox{},
		&Email{},
		&SMTPSender{},
		&MailSuppression{},
		&MailRawBlob{},
	}
}
