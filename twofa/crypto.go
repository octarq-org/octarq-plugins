package twofa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
)

var (
	// ErrDecryptionFailed is returned when ciphertext is tampered, corrupted, or key is incorrect.
	ErrDecryptionFailed = errors.New("failed to decrypt 2fa secret: authentication failed or corrupted data")
	// ErrInvalidCiphertext is returned when ciphertext string cannot be decoded or is too short.
	ErrInvalidCiphertext = errors.New("invalid ciphertext format")
)

const defaultKeySalt = "octarq-twofa-vault-encryption-salt-v1"

// GetVaultKey derives a 32-byte AES-256 key from environmental master secrets
// or a provided custom key. It uses SHA-256 with a domain-separated salt.
func GetVaultKey(customKey string) []byte {
	if customKey == "" {
		customKey = os.Getenv("OCTARQ_TWOFA_KEY")
	}
	if customKey == "" {
		customKey = os.Getenv("OCTARQ_SECRET_KEY")
	}
	if customKey == "" {
		// Fallback deterministic seed for default installations
		customKey = "octarq-default-internal-vault-seed"
	}

	h := sha256.New()
	h.Write([]byte(defaultKeySalt))
	h.Write([]byte(customKey))
	return h.Sum(nil)
}

// EncryptSecret encrypts a plaintext string (e.g. Base32 TOTP secret) using AES-256-GCM.
// Returns a URL-safe or standard Base64 string containing [12-byte nonce || ciphertext + 16-byte tag].
func EncryptSecret(plaintext string, key []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("key must be exactly 32 bytes for AES-256")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptSecret decrypts a base64-encoded AES-256-GCM ciphertext using the given 32-byte key.
func DecryptSecret(encodedCiphertext string, key []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("key must be exactly 32 bytes for AES-256")
	}

	data, err := base64.StdEncoding.DecodeString(encodedCiphertext)
	if err != nil {
		return "", ErrInvalidCiphertext
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidCiphertext
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}

	return string(plaintext), nil
}
