package mail

import (
	"time"

	"github.com/octarq-org/octarq/server/pkg/hostlist"
)

type orgEntity struct {
	ID           uint              `gorm:"primaryKey;column:id"`
	Slug         string            `gorm:"column:slug"`
	InboundToken string            `gorm:"column:inbound_token"`
	MailHosts    hostlist.HostList `gorm:"column:mail_hosts"`
}

func (orgEntity) TableName() string { return "orgs" }

type auditLogEntity struct {
	ID         uint      `gorm:"primaryKey;column:id"`
	OrgID      uint      `gorm:"column:org_id"`
	ActorID    uint      `gorm:"column:actor_id"`
	Action     string    `gorm:"column:action"`
	TargetType string    `gorm:"column:target_type"`
	TargetID   uint      `gorm:"column:target_id"`
	IP         string    `gorm:"column:ip"`
	Meta       string    `gorm:"column:meta;type:text"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (auditLogEntity) TableName() string { return "audit_logs" }

type notificationChannelEntity struct {
	ID      uint   `gorm:"primaryKey;column:id"`
	OwnerID uint   `gorm:"column:owner_id"`
	Type    string `gorm:"column:type"`
	Config  string `gorm:"column:config"`
	Enabled bool   `gorm:"column:enabled"`
}

func (notificationChannelEntity) TableName() string { return "notification_channels" }
