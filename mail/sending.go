package mail

import (
	"context"
	"errors"
	"fmt"
	"log"
	netmail "net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/octarq-org/octarq-plugins/mail/pkg/mail"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

func (p *Plugin) getStorageProvider() (plugin.StorageProvider, error) {
	var sp plugin.StorageProvider
	if p.ctx != nil {
		if found, ok := plugin.LookupAs[plugin.StorageProvider](p.ctx, plugin.ServiceMailStorageProvider); ok && found != nil {
			sp = found
		}
	}

	backend := p.getBackendConfig()
	if backend != "" && backend != "database" && backend != "db" {
		if _, isDB := sp.(*DBStorageProvider); sp != nil && !isDB {
			return sp, nil
		}
		return nil, fmt.Errorf("mail storage provider %q requires Pro edition", backend)
	}

	if sp != nil {
		return sp, nil
	}
	return NewDBStorageProvider(p.db), nil
}

func (p *Plugin) getBackendConfig() string {
	if p.getGlobalSetting != nil {
		if val := strings.TrimSpace(p.getGlobalSetting("mail_storage_backend")); val != "" {
			return val
		}
	}
	return "database"
}

func (p *Plugin) vault() (plugin.VaultService, error) {
	if p.vaultSvc != nil {
		return p.vaultSvc, nil
	}
	if svc, ok := plugin.LookupAs[plugin.VaultService](p.ctx, plugin.ServiceVault); ok && svc != nil {
		return svc, nil
	}
	return nil, plugin.ErrVaultUnavailable
}

func (p *Plugin) isSuppressed(orgID uint, addr string) bool {
	if addr == "" || orgID == 0 {
		return false
	}
	normAddr := strings.ToLower(strings.TrimSpace(addr))
	if parsed, err := netmail.ParseAddress(normAddr); err == nil && parsed.Address != "" {
		normAddr = strings.ToLower(parsed.Address)
	}
	tdb := p.tenantDB(orgID)
	if tdb == nil {
		return false
	}
	var count int64
	tdb.Model(&MailSuppression{}).Where("address = ?", normAddr).Count(&count)
	return count > 0
}

func (p *Plugin) anySuppressed(orgID uint, addrs []string) (string, error) {
	if len(addrs) == 0 || orgID == 0 {
		return "", nil
	}
	normAddrs := make([]string, 0, len(addrs))

	for _, addr := range addrs {
		normAddr := strings.ToLower(strings.TrimSpace(addr))
		if parsed, err := netmail.ParseAddress(normAddr); err == nil && parsed.Address != "" {
			normAddr = strings.ToLower(parsed.Address)
		}
		if normAddr != "" {
			normAddrs = append(normAddrs, normAddr)
		}
	}

	if len(normAddrs) == 0 {
		return "", nil
	}

	tdb := p.tenantDB(orgID)
	if tdb == nil {
		return "", nil
	}
	var suppressed []string
	err := tdb.Model(&MailSuppression{}).Where("address IN ?", normAddrs).Pluck("address", &suppressed).Error
	if err != nil {
		return "", err
	}

	if len(suppressed) > 0 {
		return suppressed[0], nil
	}
	return "", nil
}

func strFromAny(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

func (p *Plugin) senderFromVaultSecret(sec *plugin.VaultSecret) (mail.Sender, string, error) {
	if sec == nil || len(sec.Payload) == 0 {
		return nil, "", errors.New("empty credential configuration")
	}

	from := strings.TrimSpace(strFromAny(sec.Payload["fromEmail"]))
	if from == "" {
		return nil, "", errors.New("sender From address is not configured")
	}

	if sec.Meta.Provider == "resend" {
		apiKey := strFromAny(sec.Payload["apiKey"])
		if apiKey == "" {
			apiKey = strFromAny(sec.Payload["secretKey"])
		}
		if apiKey == "" {
			return nil, "", errors.New("resend API key is required")
		}
		return mail.NewResendSender(apiKey, from), from, nil
	}

	host := strFromAny(sec.Payload["smtpHost"])
	if host == "" {
		host = strFromAny(sec.Payload["host"])
	}
	if host == "" {
		return nil, "", errors.New("smtp host is required")
	}
	port := intFromAny(sec.Payload["smtpPort"])
	if port == 0 {
		port = intFromAny(sec.Payload["port"])
	}
	if port == 0 {
		port = 587
	}
	user := strFromAny(sec.Payload["user"])
	if user == "" {
		user = strFromAny(sec.Payload["apiKey"])
	}
	pass := strFromAny(sec.Payload["pass"])
	if pass == "" {
		pass = strFromAny(sec.Payload["secretKey"])
	}

	return mail.NewCustomSender(host, fmt.Sprint(port), user, pass, from), from, nil
}

func (p *Plugin) resolveSenderPassword(ctx context.Context, s *SMTPSender) (string, error) {
	if s.VaultItemID == 0 {
		return "", errors.New("SMTP sender credentials are not configured")
	}
	vs, err := p.vault()
	if err != nil {
		return "", err
	}
	var sec *plugin.VaultSecret
	if s.OrgID == 0 {
		items, lErr := vs.ListItems(ctx, 0, plugin.VaultFilter{
			Scope:        plugin.VaultScopeInstance,
			ProviderType: "communication",
		})
		if lErr != nil {
			return "", lErr
		}
		var keyName string
		for _, it := range items {
			if it.ID == s.VaultItemID {
				keyName = it.KeyName
				break
			}
		}
		if keyName == "" {
			return "", plugin.ErrSecretNotFound
		}
		sec, err = vs.ResolveInstanceSecret(ctx, keyName)
	} else {
		sec, err = vs.GetSecret(ctx, s.OrgID, s.VaultItemID)
	}
	if err != nil {
		return "", err
	}
	if sec == nil {
		return "", plugin.ErrSecretNotFound
	}
	pass := strFromAny(sec.Payload["secretKey"])
	if pass == "" {
		pass = strFromAny(sec.Payload["pass"])
	}
	return pass, nil
}

func (p *Plugin) mailReady() bool {
	_, err := p.systemSender()
	return err == nil
}

func (p *Plugin) systemSender() (*SMTPSender, error) {
	if p.getGlobalSetting != nil {
		if idStr := strings.TrimSpace(p.getGlobalSetting("mail_system_sender_id")); idStr != "" {
			if id, err := strconv.ParseUint(idStr, 10, 64); err == nil && id != 0 {
				var s SMTPSender
				if err := p.db.First(&s, id).Error; err == nil {
					return &s, nil
				}
				if vs, vErr := p.vault(); vErr == nil {
					items, lErr := vs.ListItems(context.Background(), 0, plugin.VaultFilter{
						Scope:        plugin.VaultScopeInstance,
						ProviderType: "communication",
					})
					if lErr == nil {
						for _, it := range items {
							if it.ID == uint(id) && (it.Provider == "smtp" || it.Provider == "resend") && it.KeyName != "" {
								return &SMTPSender{
									ID:          it.ID,
									OrgID:       0,
									Name:        it.Name,
									VaultItemID: it.ID,
								}, nil
							}
						}
					}
				}
				return nil, fmt.Errorf("configured system SMTP sender %d not found: %w", id, gorm.ErrRecordNotFound)
			}
			return nil, fmt.Errorf("invalid mail_system_sender_id setting %q", idStr)
		}
	}
	return nil, fmt.Errorf("no SMTP sender configured on this instance; configure mail_system_sender_id in system settings")
}

func (p *Plugin) deliverVia(s *SMTPSender, to, subject, htmlBody, textBody string) error {
	ctx := context.Background()
	var sender mail.Sender
	from := s.FromEmail

	if s.Host == "" && s.OrgID == 0 && s.VaultItemID > 0 {
		vs, err := p.vault()
		if err != nil {
			return err
		}
		items, err := vs.ListItems(ctx, 0, plugin.VaultFilter{
			Scope:        plugin.VaultScopeInstance,
			ProviderType: "communication",
		})
		if err != nil {
			return err
		}
		var keyName string
		for _, it := range items {
			if it.ID == s.VaultItemID {
				keyName = it.KeyName
				break
			}
		}
		if keyName == "" {
			return plugin.ErrSecretNotFound
		}
		sec, err := vs.ResolveInstanceSecret(ctx, keyName)
		if err != nil {
			return err
		}
		var sErr error
		sender, from, sErr = p.senderFromVaultSecret(sec)
		if sErr != nil {
			return sErr
		}
	} else {
		pass, err := p.resolveSenderPassword(ctx, s)
		if err != nil {
			return err
		}
		sender = mail.NewCustomSender(s.Host, fmt.Sprint(s.Port), s.User, pass, from)
	}

	if err := sender.Send(mail.Message{From: from, To: []string{to}, Subject: subject, HTML: htmlBody, Text: textBody}); err != nil {
		if p.publishEvent != nil {
			p.publishEvent(s.OrgID, "email.send_failed", map[string]any{"to": []string{to}, "subject": subject, "error": err.Error()})
		}
		return err
	}
	if p.recordUsage != nil {
		p.recordUsage(s.OrgID, plugin.MetricMailOut, 1)
	}
	return nil
}

func (p *Plugin) sendSystemMail(to, subject, htmlBody, textBody string) error {
	s, err := p.systemSender()
	if err != nil {
		return err
	}
	return p.deliverVia(s, to, subject, htmlBody, textBody)
}

func (p *Plugin) sendMail(orgID uint, to, subject, htmlBody, textBody string) error {
	if p.isSuppressed(orgID, to) {
		return fmt.Errorf("recipient address %s is in suppression list", to)
	}

	ctx := context.Background()
	vs, err := p.vault()
	if err != nil {
		return err
	}

	items, err := vs.ListItems(ctx, orgID, plugin.VaultFilter{
		Scope:        plugin.VaultScopeOrg,
		ProviderType: "communication",
	})
	if err != nil {
		return fmt.Errorf("resolve mail integration for org %d: %w", orgID, err)
	}

	for _, item := range items {
		if item.Provider != "smtp" && item.Provider != "resend" {
			continue
		}
		sec, secErr := vs.GetSecret(ctx, orgID, item.ID)
		if secErr != nil {
			return fmt.Errorf("resolve %s integration for org %d: %w", item.Provider, orgID, secErr)
		}
		sender, from, sErr := p.senderFromVaultSecret(sec)
		if sErr != nil {
			return fmt.Errorf("invalid %s integration for org %d: %w", item.Provider, orgID, sErr)
		}
		if err := sender.Send(mail.Message{From: from, To: []string{to}, Subject: subject, HTML: htmlBody, Text: textBody}); err != nil {
			if p.publishEvent != nil {
				p.publishEvent(orgID, "email.send_failed", map[string]any{"to": []string{to}, "subject": subject, "error": err.Error()})
			}
			return err
		}
		if p.recordUsage != nil {
			p.recordUsage(orgID, plugin.MetricMailOut, 1)
		}
		return nil
	}

	return fmt.Errorf("no SMTP or Resend integration is configured for org %d", orgID)
}

func (p *Plugin) recordSentEmail(orgID uint, from, to, subject, text, html string) {
	tdb := p.tenantDB(orgID)
	if tdb == nil {
		return
	}
	var mb Mailbox
	if err := tdb.Where("address = ?", from).First(&mb).Error; err != nil {
		if err := tdb.First(&mb).Error; err != nil {
			mb = Mailbox{OrgID: orgID, Address: from, Enabled: true, Note: "outbound"}
			if err := tdb.Create(&mb).Error; err != nil {
				log.Printf("mail: failed to auto-create outbound mailbox for org %d: %v", orgID, err)
			}
		}
	}
	if mb.ID != 0 {
		sentEmail := Email{
			MailboxID:  mb.ID,
			FromAddr:   from,
			ToAddr:     to,
			Subject:    subject,
			Text:       text,
			HTML:       html,
			Folder:     "sent",
			Read:       true,
			ReceivedAt: time.Now(),
		}
		if err := p.db.Create(&sentEmail).Error; err != nil {
			log.Printf("mail: failed to persist sent email record for mailbox %d: %v", mb.ID, err)
		}
	}
}
