package mail

import (
	"time"
)

type Mailbox struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	OrgID     uint      `gorm:"column:owner_id;index:idx_mailbox_owner_address,unique;default:1" json:"-"`
	Address   string    `gorm:"size:320;index:idx_mailbox_owner_address,unique" json:"address"`
	Note      string    `gorm:"type:text" json:"note"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Unread    int64     `gorm:"-" json:"unread"`
}

type Email struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	MailboxID      uint      `gorm:"index" json:"mailboxId"`
	MessageID      string    `gorm:"size:512;index" json:"messageId"`
	FromAddr       string    `gorm:"size:320" json:"from"`
	ToAddr         string    `gorm:"size:320" json:"to"`
	Subject        string    `gorm:"type:text" json:"subject"`
	Text           string    `gorm:"type:text" json:"text"`
	HTML           string    `gorm:"type:text" json:"html"`
	Raw            []byte    `json:"-"`
	StorageKey     string    `gorm:"size:255;index" json:"-"`
	Read           bool      `gorm:"default:false" json:"read"`
	Note           string    `gorm:"type:text" json:"note"`
	Attachments    string    `gorm:"type:text" json:"attachments"`
	AuthSPF        string    `gorm:"size:16" json:"authSpf"`
	AuthDKIM       string    `gorm:"size:16" json:"authDkim"`
	AuthDMARC      string    `gorm:"size:16" json:"authDmarc"`
	Folder         string    `gorm:"size:32;index;default:'inbox'" json:"folder"`
	UnsubscribeURL string    `gorm:"size:1024" json:"unsubscribeUrl"`
	ReceivedAt     time.Time `gorm:"index" json:"receivedAt"`
	Category       string    `gorm:"size:32;index" json:"category,omitempty"`
	Summary        string    `gorm:"type:text" json:"summary,omitempty"`
	Importance     int       `gorm:"default:0" json:"importance,omitempty"`
	OTP            string    `gorm:"-" json:"otp,omitempty"`
}

type SMTPSender struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	OrgID       uint      `gorm:"column:owner_id;index" json:"-"`
	Name        string    `json:"name"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	User        string    `json:"user"`
	VaultItemID uint      `gorm:"index" json:"vaultItemId,omitempty"`
	FromEmail   string    `json:"fromEmail"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	PassSet     bool      `gorm:"-" json:"passSet"`
}

type MailSuppression struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	OrgID     uint      `gorm:"column:owner_id;index:idx_mail_suppression_owner_address,unique;default:1" json:"-"`
	Address   string    `gorm:"size:320;index:idx_mail_suppression_owner_address,unique" json:"address"`
	Reason    string    `gorm:"size:32" json:"reason"`
	Source    string    `gorm:"type:text" json:"source"`
	Count     int       `gorm:"default:1" json:"count"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type MailContact struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	OrgID            uint      `gorm:"column:owner_id;index:idx_contact_org_addr,unique;default:1" json:"-"`
	Address          string    `gorm:"size:320;index:idx_contact_org_addr,unique" json:"address"`
	Name             string    `gorm:"size:255" json:"name"`
	InteractionCount int       `gorm:"default:1" json:"interactionCount"`
	LastSeenAt       time.Time `gorm:"index" json:"lastSeenAt"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type MailDraft struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	OrgID       uint      `gorm:"column:owner_id;index;default:1" json:"-"`
	MailboxID   uint      `gorm:"index" json:"mailboxId,omitempty"`
	To          string    `gorm:"size:1024" json:"to"`
	CC          string    `gorm:"size:1024" json:"cc,omitempty"`
	Subject     string    `gorm:"type:text" json:"subject"`
	Body        string    `gorm:"type:text" json:"body"`
	InReplyToID uint      `gorm:"index" json:"inReplyToId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (MailDraft) TableName() string { return "mail_drafts" }
