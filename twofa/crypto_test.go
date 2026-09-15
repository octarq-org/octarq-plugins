package twofa

import (
	"encoding/base64"
	"testing"
)

func TestCrypto_EncryptDecrypt(t *testing.T) {
	key := GetVaultKey("test-master-key-12345")
	secret := "JBSWY3DPEHPK3PXP"

	ciphertext, err := EncryptSecret(secret, key)
	if err != nil {
		t.Fatalf("EncryptSecret failed: %v", err)
	}

	if ciphertext == secret {
		t.Fatalf("Ciphertext should not equal plaintext")
	}

	// Two encryptions must have different ciphertexts due to random nonces
	ciphertext2, err := EncryptSecret(secret, key)
	if err != nil {
		t.Fatalf("Second EncryptSecret failed: %v", err)
	}
	if ciphertext == ciphertext2 {
		t.Fatalf("Two encryptions produced identical ciphertexts; nonce was not randomized")
	}

	decrypted, err := DecryptSecret(ciphertext, key)
	if err != nil {
		t.Fatalf("DecryptSecret failed: %v", err)
	}

	if decrypted != secret {
		t.Fatalf("Decrypted secret mismatch: got %q, want %q", decrypted, secret)
	}
}

func TestCrypto_TamperedCiphertext(t *testing.T) {
	key := GetVaultKey("test-tamper-key")
	secret := "MYSECRETBASE32KEY"

	ciphertext, err := EncryptSecret(secret, key)
	if err != nil {
		t.Fatalf("EncryptSecret failed: %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("DecodeString failed: %v", err)
	}

	// Tamper with ciphertext payload
	raw[len(raw)-1] ^= 0xFF
	tampered := base64.StdEncoding.EncodeToString(raw)

	_, err = DecryptSecret(tampered, key)
	if err == nil {
		t.Fatalf("Expected DecryptSecret to fail on tampered data, but succeeded")
	}
}

func TestCrypto_WrongKey(t *testing.T) {
	key1 := GetVaultKey("key-one")
	key2 := GetVaultKey("key-two")
	secret := "MYSECRETBASE32KEY"

	ciphertext, err := EncryptSecret(secret, key1)
	if err != nil {
		t.Fatalf("EncryptSecret failed: %v", err)
	}

	_, err = DecryptSecret(ciphertext, key2)
	if err == nil {
		t.Fatalf("Expected DecryptSecret to fail with wrong key, but succeeded")
	}
}

func TestCrypto_InvalidKeyLength(t *testing.T) {
	shortKey := []byte("too-short")
	_, err := EncryptSecret("test", shortKey)
	if err == nil {
		t.Fatalf("Expected error for key length != 32")
	}

	_, err = DecryptSecret("YWJj", shortKey)
	if err == nil {
		t.Fatalf("Expected error for key length != 32")
	}
}
