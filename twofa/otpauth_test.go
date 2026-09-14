package twofa

import (
	"bytes"
	"strings"
	"testing"
)

func TestOTPAuth_ParseURI(t *testing.T) {
	uri := "otpauth://totp/GitHub:octocat?secret=JBSWY3DPEHPK3PXP&issuer=GitHub&algorithm=SHA1&digits=6&period=30"
	acc, secret, err := ParseOTPAuthURI(uri)
	if err != nil {
		t.Fatalf("ParseOTPAuthURI failed: %v", err)
	}

	if acc.Issuer != "GitHub" {
		t.Errorf("Issuer = %q, want GitHub", acc.Issuer)
	}
	if acc.Account != "octocat" {
		t.Errorf("Account = %q, want octocat", acc.Account)
	}
	if secret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("Secret = %q, want JBSWY3DPEHPK3PXP", secret)
	}
	if acc.Algorithm != "SHA1" {
		t.Errorf("Algorithm = %q, want SHA1", acc.Algorithm)
	}
	if acc.Digits != 6 {
		t.Errorf("Digits = %d, want 6", acc.Digits)
	}
	if acc.Period != 30 {
		t.Errorf("Period = %d, want 30", acc.Period)
	}
}

func TestOTPAuth_ParseURI_WithoutIssuer(t *testing.T) {
	uri := "otpauth://totp/admin@example.com?secret=JBSWY3DPEHPK3PXP"
	acc, secret, err := ParseOTPAuthURI(uri)
	if err != nil {
		t.Fatalf("ParseOTPAuthURI failed: %v", err)
	}

	if acc.Account != "admin@example.com" {
		t.Errorf("Account = %q, want admin@example.com", acc.Account)
	}
	if secret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("Secret = %q, want JBSWY3DPEHPK3PXP", secret)
	}
	if acc.Period != 30 {
		t.Errorf("Default period should be 30, got %d", acc.Period)
	}
	if acc.Digits != 6 {
		t.Errorf("Default digits should be 6, got %d", acc.Digits)
	}
}

func TestOTPAuth_BuildURI(t *testing.T) {
	acc := &TwoFAAccount{
		Name:      "AWS Root",
		Issuer:    "Amazon Web Services",
		Account:   "root@octarq.com",
		Algorithm: "SHA1",
		Digits:    6,
		Period:    30,
	}
	secret := "JBSWY3DPEHPK3PXP"

	uri := BuildOTPAuthURI(acc, secret)
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("URI should start with otpauth://totp/, got %q", uri)
	}
	if !strings.Contains(uri, "secret=JBSWY3DPEHPK3PXP") {
		t.Errorf("URI missing secret: %s", uri)
	}
	if !strings.Contains(uri, "issuer=Amazon+Web+Services") && !strings.Contains(uri, "issuer=Amazon%20Web%20Services") {
		t.Errorf("URI missing issuer: %s", uri)
	}

	// Re-parsing built URI should match
	parsed, parsedSec, err := ParseOTPAuthURI(uri)
	if err != nil {
		t.Fatalf("Re-parsing built URI failed: %v", err)
	}
	if parsedSec != secret {
		t.Errorf("Re-parsed secret = %q, want %q", parsedSec, secret)
	}
	if parsed.Issuer != acc.Issuer {
		t.Errorf("Re-parsed issuer = %q, want %q", parsed.Issuer, acc.Issuer)
	}
}

func TestOTPAuth_QRCode(t *testing.T) {
	uri := "otpauth://totp/Test:user?secret=JBSWY3DPEHPK3PXP"

	pngBytes, err := GenerateQRCodePNG(uri, 128)
	if err != nil {
		t.Fatalf("GenerateQRCodePNG failed: %v", err)
	}

	// PNG file signature: \x89PNG\r\n\x1a\n
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	if !bytes.HasPrefix(pngBytes, pngHeader) {
		t.Fatalf("Generated data is not a valid PNG")
	}

	dataURI, err := GenerateQRCodeDataURI(uri, 128)
	if err != nil {
		t.Fatalf("GenerateQRCodeDataURI failed: %v", err)
	}
	if !strings.HasPrefix(dataURI, "data:image/png;base64,") {
		t.Fatalf("Data URI should start with data:image/png;base64, got %s", dataURI[:30])
	}
}
