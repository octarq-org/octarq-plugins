package twofa

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"math"
	"strings"
	"time"
)

var (
	ErrInvalidSecret   = errors.New("invalid base32 secret")
	ErrUnsupportedAlgo = errors.New("unsupported algorithm, must be SHA1, SHA256, or SHA512")
	ErrInvalidDigits   = errors.New("digits must be between 6 and 8")
	ErrInvalidPeriod   = errors.New("period must be greater than 0")
)

// NormalizeSecret cleans user input secret by removing whitespace and hyphens,
// converting to uppercase, and ensuring proper Base32 padding.
func NormalizeSecret(s string) string {
	clean := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "-", ""))
	// RFC 4648 Base32 chunks of 8 characters
	rem := len(clean) % 8
	if rem != 0 {
		clean += strings.Repeat("=", 8-rem)
	}
	return clean
}

// DecodeBase32 decodes a secret string formatted as standard Base32.
func DecodeBase32(secret string) ([]byte, error) {
	norm := NormalizeSecret(secret)
	data, err := base32.StdEncoding.DecodeString(norm)
	if err != nil {
		// Try unpadded decoding
		data, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(norm, "="))
		if err != nil {
			return nil, ErrInvalidSecret
		}
	}
	if len(data) == 0 {
		return nil, ErrInvalidSecret
	}
	return data, nil
}

// GenerateCode computes the RFC 6238 TOTP code for a given secret at timestamp t.
func GenerateCode(secret string, t time.Time, period int, digits int, algorithm string) (string, error) {
	if period <= 0 {
		period = 30
	}
	if digits < 6 || digits > 8 {
		digits = 6
	}
	if algorithm == "" {
		algorithm = "SHA1"
	}

	key, err := DecodeBase32(secret)
	if err != nil {
		return "", err
	}

	var h func() hash.Hash
	switch strings.ToUpper(algorithm) {
	case "SHA1", "SHA-1":
		h = sha1.New
	case "SHA256", "SHA-256":
		h = sha256.New
	case "SHA512", "SHA-512":
		h = sha512.New
	default:
		return "", ErrUnsupportedAlgo
	}

	counter := uint64(t.Unix()) / uint64(period)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	mac := hmac.New(h, key)
	mac.Write(buf)
	sum := mac.Sum(nil)

	// RFC 4226 Dynamic Truncation
	offset := sum[len(sum)-1] & 0x0f
	binCode := (int(sum[offset]&0x7f) << 24) |
		(int(sum[offset+1]&0xff) << 16) |
		(int(sum[offset+2]&0xff) << 8) |
		int(sum[offset+3]&0xff)

	mod := int(math.Pow10(digits))
	otp := binCode % mod

	format := fmt.Sprintf("%%0%dd", digits)
	return fmt.Sprintf(format, otp), nil
}

// VerifyCode verifies whether the given code matches the TOTP code at time t,
// allowing for a clock drift window (drift steps before and after).
func VerifyCode(secret string, code string, t time.Time, period int, digits int, algorithm string, drift int) bool {
	if period <= 0 {
		period = 30
	}
	if digits < 6 || digits > 8 {
		digits = 6
	}
	if drift < 0 {
		drift = 1
	}

	code = strings.TrimSpace(code)
	if len(code) != digits {
		return false
	}

	currentTime := t
	for d := -drift; d <= drift; d++ {
		targetTime := currentTime.Add(time.Duration(d*period) * time.Second)
		gen, err := GenerateCode(secret, targetTime, period, digits, algorithm)
		if err == nil && subtle.ConstantTimeCompare([]byte(gen), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// SecondsRemaining calculates how many seconds remain before the current TOTP step expires.
func SecondsRemaining(t time.Time, period int) int {
	if period <= 0 {
		period = 30
	}
	rem := period - int(t.Unix()%int64(period))
	if rem <= 0 {
		rem = period
	}
	return rem
}
