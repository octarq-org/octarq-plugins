package mail

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq-plugins/mail/pkg/mail"
	"github.com/octarq-org/octarq/server/pkg/safego"
	"github.com/octarq-org/octarq/server/plugin"
)

type InboundInput struct {
	Ctx     huma.Context `hidden:"true"`
	OrgSlug string       `path:"orgSlug"`
	Token   string       `path:"token"`
}

func (i *InboundInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type InboundOutput struct {
	Body map[string]any
}

func (p *Plugin) inbound(ctx context.Context, input *InboundInput) (*InboundOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("An unexpected error occurred while processing the request context.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	var org orgEntity
	if p.db.Where("slug = ?", input.OrgSlug).First(&org).Error != nil {
		return nil, huma.Error404NotFound("Workspace not found.")
	}
	if org.InboundToken == "" || subtle.ConstantTimeCompare([]byte(input.Token), []byte(org.InboundToken)) != 1 {
		p.recordInboundAuthFailure(r, org.ID, "path-token")
		return nil, huma.Error401Unauthorized("Invalid webhook token. Provide a valid inbound webhook token.")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 25<<20))
	if err != nil {
		return nil, huma.Error400BadRequest("Could not read webhook request body.")
	}
	overrideTo := r.Header.Get("X-Octarq-To")
	return p.processInboundMail(ctx, org.ID, overrideTo, raw)
}

func (p *Plugin) processInboundMail(ctx context.Context, orgID uint, overrideTo string, raw []byte) (*InboundOutput, error) {
	ctx = plugin.WithOrgID(ctx, orgID)
	parsed, parseErr := mail.Parse(raw)
	if parseErr != nil {
		log.Printf("inbound: mail parse failed: %v", parseErr)
	}

	to := strings.ToLower(strings.TrimSpace(overrideTo))
	if to == "" && parsed != nil {
		to = strings.ToLower(parsed.To)
	}

	mb, ok := p.resolveMailbox(orgID, to)
	if !ok {
		log.Printf("inbound: dropped message for unmanaged recipient %q in org %d", to, orgID)
		return &InboundOutput{Body: map[string]any{"ok": true, "stored": false}}, nil
	}

	att := ""
	if parsed != nil && len(parsed.Attachments) > 0 {
		if b, err := json.Marshal(parsed.Attachments); err == nil {
			att = string(b)
		}
	}

	msgID := ""
	from := ""
	subject := ""
	textBody := ""
	htmlBody := ""
	var receivedAt time.Time
	spf, dkim, dmarc := "", "", ""

	if parsed != nil {
		msgID = parsed.MessageID
		from = parsed.From
		subject = parsed.Subject
		textBody = parsed.Text
		htmlBody = parsed.HTML
		receivedAt = parsed.ReceivedAt
		spf = parsed.Auth.SPF
		dkim = parsed.Auth.DKIM
		dmarc = parsed.Auth.DMARC
	}

	if msgID != "" {
		var existing Email
		if p.db.Where("mailbox_id = ? AND message_id = ?", mb.ID, msgID).First(&existing).Error == nil {
			return &InboundOutput{Body: map[string]any{"ok": true, "stored": true, "id": existing.ID, "duplicate": true}}, nil
		}
	}

	unsubURL := ExtractUnsubscribeURL(raw)
	p.upsertContact(mb.OrgID, from)

	e := Email{
		MailboxID:      mb.ID,
		MessageID:      msgID,
		FromAddr:       from,
		ToAddr:         to,
		Subject:        subject,
		Text:           textBody,
		HTML:           htmlBody,
		Folder:         "inbox",
		UnsubscribeURL: unsubURL,
		Attachments:    att,
		ReceivedAt:     receivedAt,
		AuthSPF:        spf,
		AuthDKIM:       dkim,
		AuthDMARC:      dmarc,
	}
	if err := p.db.Create(&e).Error; err != nil {
		log.Printf("inbound: failed to store email: %v", err)
		return nil, huma.Error500InternalServerError("Could not store inbound email. Try again later.")
	}

	key := fmt.Sprintf("mail/%d/%d.eml", mb.OrgID, e.ID)
	storageProv, spErr := p.getStorageProvider()
	if spErr != nil {
		log.Printf("inbound: storage provider configuration error (%v); falling back to the database", spErr)
		storageProv = NewDBStorageProvider(p.db)
	}

	if putErr := storageProv.Put(ctx, key, raw); putErr != nil {
		log.Printf("mail storage: Put failed for key %s, falling back to the database: %v", key, putErr)
		p.db.Model(&e).Update("raw", raw)
	} else {
		p.db.Model(&e).Update("storage_key", key)
	}

	if p.recordUsage != nil {
		p.recordUsage(mb.OrgID, plugin.MetricRawBytes, int64(len(raw)))
		p.recordUsage(mb.OrgID, plugin.MetricMailIn, 1)
	}

	if p.host != nil && p.host.Events() != nil {
		p.host.Events().PublishEvent(mb.OrgID, "email.receive", map[string]any{
			"emailId":    e.ID,
			"mailboxId":  mb.ID,
			"from":       e.FromAddr,
			"to":         e.ToAddr,
			"subject":    e.Subject,
			"receivedAt": e.ReceivedAt,
		})
	}

	p.emitEmail(plugin.EmailEvent{
		ID:         e.ID,
		MailboxID:  mb.ID,
		OrgID:      mb.OrgID,
		From:       from,
		To:         to,
		Subject:    subject,
		Text:       textBody,
		HTML:       htmlBody,
		ReceivedAt: receivedAt,
	})

	text := fmt.Sprintf("📧 New mail to %s from %s: %s", to, from, subject)
	if p.emit != nil {
		safego.Go("mail.inbound-notify", func() {
			ctxCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := p.emit(ctxCtx, plugin.NotificationPayload{
				EventType: "mail.inbound",
				Title:     fmt.Sprintf("New mail to %s", to),
				Body:      text,
				OrgID:     strconv.FormatUint(uint64(mb.OrgID), 10),
				Priority:  "normal",
				Data: map[string]any{
					"mailboxId": mb.ID,
					"to":        to,
					"from":      from,
					"subject":   subject,
				},
			}); err != nil {
				log.Printf("mail: failed to emit inbound notification for mailbox %d: %v", mb.ID, err)
			}
		})
	} else if p.notify != nil {
		var channels []notificationChannelEntity
		p.db.Where("owner_id = ? AND enabled = ?", mb.OrgID, true).Find(&channels)
		if len(channels) > 0 {
			safego.Go("mail.inbound-notify", func() {
				ctxCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				for _, ch := range channels {
					if err := p.notify(ctxCtx, ch.Type, ch.Config, text); err != nil {
						log.Printf("mail: failed to notify channel %s for inbound mail: %v", ch.Type, err)
					}
				}
			})
		}
	}

	return &InboundOutput{Body: map[string]any{"ok": true, "stored": true, "id": e.ID}}, nil
}
