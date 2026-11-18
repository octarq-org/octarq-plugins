package mail

import (
	"context"
	"errors"
	"fmt"

	"github.com/octarq-org/octarq-plugins/mail/pkg/mail"
	"github.com/octarq-org/octarq/server/plugin"
)

// Sender registry identity. The local mail_smtp_senders table and the
// Integrations Hub number their rows independently, so an ID identifies a
// sender only together with its source.
const (
	SenderSourceLocal       = "local"
	SenderSourceIntegration = "integration"
)

// Resolver failure kinds. Callers map them to their own error shape (HTTP for
// the REST handler, AgentError for the MCP tool).
var (
	errSenderSourceRequired = errors.New("sender source is required")
	errSenderNotFound       = errors.New("sender not found")
	errSenderUnavailable    = errors.New("sender registry unavailable")
	errSenderNotMail        = errors.New("sender cannot send email")
	errSenderMisconfigured  = errors.New("sender is misconfigured")
)

// senderResolveError carries a user-facing message next to a stable kind.
type senderResolveError struct {
	kind   error
	detail string
}

func (e *senderResolveError) Error() string { return e.detail }
func (e *senderResolveError) Unwrap() error { return e.kind }

func senderErr(kind error, detail string) error {
	return &senderResolveError{kind: kind, detail: detail}
}

// resolveSender resolves an explicitly selected sender from exactly one
// registry. It never falls back to the other registry: a stale local row must
// not shadow the integration the caller selected, and vice versa.
func (p *Plugin) resolveSender(ctx context.Context, orgID, id uint, source string) (mail.Sender, string, error) {
	if id == 0 {
		return nil, "", senderErr(errSenderNotFound, "No SMTP sender selected. Choose a configured SMTP sender.")
	}

	switch source {
	case SenderSourceIntegration:
		vs, vErr := p.vault()
		if vErr != nil {
			return nil, "", senderErr(errSenderUnavailable, "Credential vault is unavailable. Try again later.")
		}
		sec, err := vs.GetSecret(ctx, orgID, id)
		if err != nil {
			if errors.Is(err, plugin.ErrSecretNotFound) {
				return nil, "", senderErr(errSenderNotFound, "The selected integration does not exist. Select a sender from the Vault.")
			}
			return nil, "", senderErr(errSenderUnavailable, "Could not resolve SMTP sender credentials. Try again later.")
		}
		if sec == nil || sec.Meta.OrgID != orgID || sec.Meta.ProviderType != "communication" {
			return nil, "", senderErr(errSenderNotFound, "The selected integration does not exist. Select a sender from the Vault.")
		}
		// Only a communication integration that actually carries mail
		// credentials can send: a Telegram or Slack integration that shares
		// the numeric ID must not be used as a relay.
		if sec.Meta.Provider != "smtp" && sec.Meta.Provider != "resend" {
			return nil, "", senderErr(errSenderNotMail, "The selected integration cannot send email. Select an SMTP or Resend integration.")
		}
		s, from, sErr := p.senderFromVaultSecret(sec)
		if sErr != nil {
			return nil, "", senderErr(errSenderMisconfigured, "The selected SMTP sender is misconfigured: "+sErr.Error())
		}
		return s, from, nil

	case SenderSourceLocal:
		var s SMTPSender
		if err := p.db.WithContext(ctx).Where("id = ? AND owner_id = ?", id, orgID).First(&s).Error; err != nil {
			return nil, "", senderErr(errSenderNotFound, "Configured SMTP sender could not be found for this workspace.")
		}
		pass, err := p.resolveSenderPassword(ctx, &s)
		if err != nil {
			return nil, "", senderErr(errSenderUnavailable, "Could not resolve SMTP sender credentials. Update sender credentials.")
		}
		return mail.NewCustomSender(s.Host, fmt.Sprint(s.Port), s.User, pass, s.FromEmail), s.FromEmail, nil

	default:
		return nil, "", senderErr(errSenderSourceRequired, "Sender source is required. Choose a local sender or an integration.")
	}
}
