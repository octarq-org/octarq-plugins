package mail

import (
	"strings"
	"testing"

	"github.com/octarq-org/octarq/server/plugin"
)

// TestSenderFromVaultSecretRejectsIncompleteConfig pins the fail-closed rules of
// the Vault path. A sender without a From address or without a relay would either
// be rejected by the relay or send under a domain the operator does not control,
// so every gap is an error here.
func TestSenderFromVaultSecretRejectsIncompleteConfig(t *testing.T) {
	p := New()

	cases := []struct {
		name string
		sec  *plugin.VaultSecret
		want string
	}{
		{"nil secret", nil, "empty credential configuration"},
		{"empty payload", &plugin.VaultSecret{Meta: plugin.VaultItemMeta{ID: 1}}, "empty credential configuration"},
		{"missing from", &plugin.VaultSecret{
			Meta:    plugin.VaultItemMeta{ID: 1, Provider: "smtp"},
			Payload: map[string]any{"host": "smtp.example.com"},
		}, "sender From address is not configured"},
		{"resend without key", &plugin.VaultSecret{
			Meta:    plugin.VaultItemMeta{ID: 1, Provider: "resend"},
			Payload: map[string]any{"fromEmail": "a@example.com"},
		}, "resend API key is required"},
		{"smtp without host", &plugin.VaultSecret{
			Meta:    plugin.VaultItemMeta{ID: 1, Provider: "smtp"},
			Payload: map[string]any{"fromEmail": "a@example.com"},
		}, "smtp host is required"},
	}

	for _, tc := range cases {
		if _, _, err := p.senderFromVaultSecret(tc.sec); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected %q, got %v", tc.name, tc.want, err)
		}
	}
}

// TestSenderFromVaultSecretAcceptsAliases pins the accepted key spellings.
func TestSenderFromVaultSecretAcceptsAliases(t *testing.T) {
	p := New()

	// Resend accepts apiKey or secretKey.
	for _, key := range []string{"apiKey", "secretKey"} {
		sec := &plugin.VaultSecret{
			Meta:    plugin.VaultItemMeta{ID: 1, Provider: "resend"},
			Payload: map[string]any{"fromEmail": "a@example.com", key: "re-123"},
		}
		sender, from, err := p.senderFromVaultSecret(sec)
		if err != nil || sender == nil || from != "a@example.com" {
			t.Errorf("resend via %s: expected a sender, got %v / %q", key, err, from)
		}
	}

	// SMTP accepts host or smtpHost, user or apiKey, pass or secretKey, and
	// defaults the port.
	sec := &plugin.VaultSecret{
		Meta:    plugin.VaultItemMeta{ID: 1, Provider: "smtp"},
		Payload: map[string]any{"smtpHost": "smtp.example.com", "apiKey": "u", "secretKey": "p", "fromEmail": "a@example.com"},
	}
	sender, from, err := p.senderFromVaultSecret(sec)
	if err != nil || sender == nil || from != "a@example.com" {
		t.Fatalf("smtp aliases: expected a sender, got %v / %q", err, from)
	}
}
