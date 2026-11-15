package pt

import (
	"encoding/json"
	"fmt"

	"github.com/octarq-org/octarq/server/plugin"
)

// storedSecret is the decrypted credential payload. It is never logged, never
// returned by an API handler, and never written to a column in plaintext.
type storedSecret struct {
	// Login is the account secret used for form login.
	Login  string `json:"login,omitempty"`
	Cookie string `json:"cookie,omitempty"`
	APIKey string `json:"site_api_token,omitempty"`
	Token  string `json:"token,omitempty"`
}

func (s storedSecret) empty() bool {
	return s.Login == "" && s.Cookie == "" && s.APIKey == "" && s.Token == ""
}

// credentialSubject is the per-tracker subject namespace inside the
// workspace's vault. Each tracker therefore has its own data key.
func credentialSubject(secretID string) string {
	return "pt.tracker." + secretID
}

// credentialAAD binds ciphertext to the workspace and the tracker row. Both
// values are immutable, so renaming a tracker or changing its schema cannot
// make an existing ciphertext unreadable.
func credentialAAD(orgID uint, secretID string) []byte {
	return []byte(fmt.Sprintf("pt:v1:%d:%s", orgID, secretID))
}

func (p *Plugin) vault(orgID uint, secretID string) (plugin.CryptoVault, error) {
	host := p.hostRef()
	if host == nil || host.Crypto() == nil {
		return nil, cryptoMissing("vault")
	}
	v, err := host.Crypto().SubjectVault(orgID, credentialSubject(secretID))
	if err != nil {
		return nil, fmt.Errorf("pt: open credential vault: %w", err)
	}
	if v == nil {
		return nil, cryptoMissing("vault")
	}
	return v, nil
}

// encryptSecret seals the credential payload for storage.
func (p *Plugin) encryptSecret(orgID uint, secretID string, secret storedSecret) (string, error) {
	v, err := p.vault(orgID, secretID)
	if err != nil {
		return "", err
	}
	plaintext, err := json.Marshal(secret)
	if err != nil {
		return "", fmt.Errorf("pt: encode credential: %w", err)
	}
	sealed, err := v.EncryptWithAAD(plaintext, credentialAAD(orgID, secretID))
	if err != nil {
		return "", fmt.Errorf("pt: encrypt credential: %w", err)
	}
	return sealed, nil
}

// decryptSecret opens a stored credential payload. A failure here is never
// treated as "no credential": that would silently turn a key-loss incident into
// a login failure the operator cannot diagnose.
func (p *Plugin) decryptSecret(orgID uint, secretID, ciphertext string) (storedSecret, error) {
	var secret storedSecret
	if ciphertext == "" {
		return secret, nil
	}
	v, err := p.vault(orgID, secretID)
	if err != nil {
		return secret, err
	}
	plaintext, err := v.DecryptWithAAD(ciphertext, credentialAAD(orgID, secretID))
	if err != nil {
		return secret, fmt.Errorf("pt: decrypt credential: %w", err)
	}
	if err := json.Unmarshal(plaintext, &secret); err != nil {
		return storedSecret{}, fmt.Errorf("pt: decode credential: %w", err)
	}
	return secret, nil
}

// shredSecret destroys the tracker's data key. It runs after the rows are
// hard-deleted, so the ciphertext is already gone; a failure here leaves an
// orphan key but no readable data, and is reported rather than swallowed.
func (p *Plugin) shredSecret(orgID uint, secretID string) error {
	if secretID == "" {
		return nil
	}
	host := p.hostRef()
	if host == nil || host.Crypto() == nil {
		return nil
	}
	return host.Crypto().ShredSubjectKey(orgID, credentialSubject(secretID))
}

// HasEncryptedData reports whether any credential ciphertext exists, so core
// can refuse to boot after a backup restore that lost the vault keys instead of
// serving rows nobody can decrypt.
func (p *Plugin) HasEncryptedData() (bool, error) {
	db := p.dbRef()
	if db == nil {
		return false, nil
	}
	var count int64
	if err := db.Model(&PTCredential{}).Where("ciphertext <> ''").Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
