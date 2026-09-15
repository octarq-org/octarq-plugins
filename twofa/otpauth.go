package twofa

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/skip2/go-qrcode"
)

var (
	ErrInvalidURI    = errors.New("invalid otpauth uri: must start with otpauth://totp/")
	ErrMissingSecret = errors.New("invalid otpauth uri: secret query parameter is required")
)

// ParseOTPAuthURI parses a standard Google Authenticator / Key URI string into
// a TwoFAAccount model and the raw plaintext secret.
func ParseOTPAuthURI(uriStr string) (*TwoFAAccount, string, error) {
	uriStr = strings.TrimSpace(uriStr)
	u, err := url.Parse(uriStr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse uri: %w", err)
	}

	if u.Scheme != "otpauth" || !strings.EqualFold(u.Host, "totp") {
		return nil, "", ErrInvalidURI
	}

	q := u.Query()
	rawSecret := q.Get("secret")
	if rawSecret == "" {
		return nil, "", ErrMissingSecret
	}

	// Clean secret
	normSecret := NormalizeSecret(rawSecret)
	if _, err := DecodeBase32(normSecret); err != nil {
		return nil, "", ErrInvalidSecret
	}

	label := strings.TrimPrefix(u.Path, "/")
	issuer := q.Get("issuer")
	account := ""

	// Label can be "Issuer:Account" or just "Account"
	if strings.Contains(label, ":") {
		parts := strings.SplitN(label, ":", 2)
		if issuer == "" {
			issuer = strings.TrimSpace(parts[0])
		}
		account = strings.TrimSpace(parts[1])
	} else if label != "" {
		account = label
	}

	name := label
	if name == "" {
		if issuer != "" {
			name = issuer
		} else {
			name = "Unnamed 2FA"
		}
	}

	algo := strings.ToUpper(q.Get("algorithm"))
	if algo == "" {
		algo = "SHA1"
	}

	digits := 6
	if dStr := q.Get("digits"); dStr != "" {
		if d, err := strconv.Atoi(dStr); err == nil && d >= 6 && d <= 8 {
			digits = d
		}
	}

	period := 30
	if pStr := q.Get("period"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			period = p
		}
	}

	acc := &TwoFAAccount{
		Name:      name,
		Issuer:    issuer,
		Account:   account,
		Algorithm: algo,
		Digits:    digits,
		Period:    period,
	}

	return acc, normSecret, nil
}

// BuildOTPAuthURI constructs an RFC-compliant otpauth://totp/ URI for an account and its secret.
func BuildOTPAuthURI(acc *TwoFAAccount, secret string) string {
	var label string
	if acc.Issuer != "" && acc.Account != "" {
		label = fmt.Sprintf("%s:%s", acc.Issuer, acc.Account)
	} else if acc.Name != "" {
		label = acc.Name
	} else if acc.Account != "" {
		label = acc.Account
	} else {
		label = "Octarq"
	}

	q := url.Values{}
	q.Set("secret", secret)
	if acc.Issuer != "" {
		q.Set("issuer", acc.Issuer)
	}
	if acc.Algorithm != "" && acc.Algorithm != "SHA1" {
		q.Set("algorithm", acc.Algorithm)
	}
	if acc.Digits != 0 && acc.Digits != 6 {
		q.Set("digits", strconv.Itoa(acc.Digits))
	}
	if acc.Period != 0 && acc.Period != 30 {
		q.Set("period", strconv.Itoa(acc.Period))
	}

	return fmt.Sprintf("otpauth://totp/%s?%s", url.PathEscape(label), q.Encode())
}

// GenerateQRCodePNG creates a PNG image of the QR code representing the URI.
func GenerateQRCodePNG(uriStr string, size int) ([]byte, error) {
	if size <= 0 {
		size = 256
	}
	return qrcode.Encode(uriStr, qrcode.Medium, size)
}

// GenerateQRCodeDataURI generates a Base64 data URI string (data:image/png;base64,...)
// suitable for embedding directly in an <img> tag.
func GenerateQRCodeDataURI(uriStr string, size int) (string, error) {
	pngBytes, err := GenerateQRCodePNG(uriStr, size)
	if err != nil {
		return "", err
	}
	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	return fmt.Sprintf("data:image/png;base64,%s", b64), nil
}
