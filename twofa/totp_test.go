package twofa

import (
	"testing"
	"time"
)

// RFC 6238 Appendix B test vectors
// Secret: ASCII "12345678901234567890" -> Base32: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
func TestTOTP_RFC6238TestVectors(t *testing.T) {
	rfcBase32Secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

	tests := []struct {
		timestamp int64
		wantSHA1  string
		want8Dig  string
	}{
		{59, "287082", "94287082"},
		{1111111109, "081804", "07081804"},
		{1111111111, "050471", "14050471"},
		{1234567890, "005924", "89005924"},
		{2000000000, "279037", "69279037"},
	}

	for _, tt := range tests {
		tm := time.Unix(tt.timestamp, 0)
		code6, err := GenerateCode(rfcBase32Secret, tm, 30, 6, "SHA1")
		if err != nil {
			t.Fatalf("GenerateCode 6 digits failed at %d: %v", tt.timestamp, err)
		}
		if code6 != tt.wantSHA1 {
			t.Errorf("At %d: got 6-digit code %q, want %q", tt.timestamp, code6, tt.wantSHA1)
		}

		code8, err := GenerateCode(rfcBase32Secret, tm, 30, 8, "SHA1")
		if err != nil {
			t.Fatalf("GenerateCode 8 digits failed at %d: %v", tt.timestamp, err)
		}
		if code8 != tt.want8Dig {
			t.Errorf("At %d: got 8-digit code %q, want %q", tt.timestamp, code8, tt.want8Dig)
		}
	}
}

func TestTOTP_VerifyCodeWithDrift(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Unix(1700000000, 0)

	// Current step code
	codeCurrent, err := GenerateCode(secret, now, 30, 6, "SHA1")
	if err != nil {
		t.Fatalf("GenerateCode failed: %v", err)
	}

	// Code at t - 30s (step - 1)
	codePrev, err := GenerateCode(secret, now.Add(-30*time.Second), 30, 6, "SHA1")
	if err != nil {
		t.Fatalf("GenerateCode prev failed: %v", err)
	}

	// Code at t + 30s (step + 1)
	codeNext, err := GenerateCode(secret, now.Add(30*time.Second), 30, 6, "SHA1")
	if err != nil {
		t.Fatalf("GenerateCode next failed: %v", err)
	}

	// Code at t + 60s (step + 2)
	codeFuture, err := GenerateCode(secret, now.Add(60*time.Second), 30, 6, "SHA1")
	if err != nil {
		t.Fatalf("GenerateCode future failed: %v", err)
	}

	// Current step should verify
	if !VerifyCode(secret, codeCurrent, now, 30, 6, "SHA1", 1) {
		t.Errorf("VerifyCode current step failed")
	}

	// Prev step should verify with drift=1
	if !VerifyCode(secret, codePrev, now, 30, 6, "SHA1", 1) {
		t.Errorf("VerifyCode prev step with drift=1 failed")
	}

	// Next step should verify with drift=1
	if !VerifyCode(secret, codeNext, now, 30, 6, "SHA1", 1) {
		t.Errorf("VerifyCode next step with drift=1 failed")
	}

	// Step + 2 should FAIL with drift=1
	if VerifyCode(secret, codeFuture, now, 30, 6, "SHA1", 1) {
		t.Errorf("VerifyCode step+2 should have failed with drift=1")
	}

	// Wrong code should fail
	if VerifyCode(secret, "000000", now, 30, 6, "SHA1", 1) && codeCurrent != "000000" {
		t.Errorf("VerifyCode accepted bogus code 000000")
	}
}

func TestTOTP_NormalizeSecret(t *testing.T) {
	inputWithSpacesAndHyphens := "jbsw-y3dp ehpk-3pxp"
	clean := NormalizeSecret(inputWithSpacesAndHyphens)
	if clean != "JBSWY3DPEHPK3PXP" {
		t.Errorf("NormalizeSecret got %q, want %q", clean, "JBSWY3DPEHPK3PXP")
	}

	data, err := DecodeBase32(inputWithSpacesAndHyphens)
	if err != nil {
		t.Fatalf("DecodeBase32 failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("Decoded data empty")
	}
}

func TestTOTP_SecondsRemaining(t *testing.T) {
	// 30s period, timestamp 100 -> 100 % 30 = 10 -> remaining = 20
	tm := time.Unix(100, 0)
	rem := SecondsRemaining(tm, 30)
	if rem != 20 {
		t.Errorf("SecondsRemaining got %d, want 20", rem)
	}

	// timestamp 120 -> 120 % 30 = 0 -> remaining = 30
	tm2 := time.Unix(120, 0)
	rem2 := SecondsRemaining(tm2, 30)
	if rem2 != 30 {
		t.Errorf("SecondsRemaining at boundary got %d, want 30", rem2)
	}
}
