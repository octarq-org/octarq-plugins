package mail

import (
	"strings"
	"testing"

	"github.com/octarq-org/octarq/server/plugin"
)

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

func TestSenderFromVaultSecretAcceptsAliases(t *testing.T) {
	p := New()

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

	sec := &plugin.VaultSecret{
		Meta:    plugin.VaultItemMeta{ID: 1, Provider: "smtp"},
		Payload: map[string]any{"smtpHost": "smtp.example.com", "apiKey": "u", "secretKey": "p", "fromEmail": "a@example.com"},
	}
	sender, from, err := p.senderFromVaultSecret(sec)
	if err != nil || sender == nil || from != "a@example.com" {
		t.Fatalf("smtp aliases: expected a sender, got %v / %q", err, from)
	}
}
