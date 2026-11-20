package adapter

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

type ErrorKind string

const (
	KindUnsupported   ErrorKind = "unsupported"
	KindAuth          ErrorKind = "auth"
	KindCookieExpired ErrorKind = "cookie_expired"
	KindNeedsManual   ErrorKind = "needs_manual_action"
	KindCaptcha       ErrorKind = "captcha"
	KindCloudflare    ErrorKind = "cloudflare"
	KindRateLimited   ErrorKind = "rate_limited"
	KindNetwork       ErrorKind = "network"
	KindParse         ErrorKind = "parse"
	KindUpstream      ErrorKind = "upstream"
	KindConfig        ErrorKind = "config"
)

type OpError struct {
	Op           string
	Schema       Schema
	Kind         ErrorKind
	Status       int
	RetryAfter   time.Duration
	ManualReason string
	Err          error
}

func (e *OpError) Error() string {
	msg := string(e.Kind)
	if e.Op != "" {
		msg = e.Op + ": " + msg
	}
	if e.Status != 0 {
		msg += fmt.Sprintf(" (status %d)", e.Status)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *OpError) Unwrap() error { return e.Err }

func (e *OpError) Is(target error) bool {
	var other *OpError
	if !errors.As(target, &other) {
		return false
	}
	return other.Kind != "" && e.Kind == other.Kind
}

var (
	ErrUnsupported = &OpError{Kind: KindUnsupported}
	ErrAuth        = &OpError{Kind: KindAuth}
	ErrCookieGone  = &OpError{Kind: KindCookieExpired}
	ErrNeedsManual = &OpError{Kind: KindNeedsManual}
	ErrCaptcha     = &OpError{Kind: KindCaptcha, ManualReason: "captcha"}
	ErrCloudflare  = &OpError{Kind: KindCloudflare, ManualReason: "cloudflare"}
	ErrRateLimited = &OpError{Kind: KindRateLimited}
	ErrNetwork     = &OpError{Kind: KindNetwork}
	ErrParse       = &OpError{Kind: KindParse}
	ErrUpstream    = &OpError{Kind: KindUpstream}
	ErrConfig      = &OpError{Kind: KindConfig}
)

func KindOf(err error) ErrorKind {
	var op *OpError
	if errors.As(err, &op) {
		return op.Kind
	}
	return ""
}

func ManualReasonOf(err error) string {
	var op *OpError
	if errors.As(err, &op) {
		return op.ManualReason
	}
	return ""
}

func statusKind(status int) ErrorKind {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return KindAuth
	case status == http.StatusTooManyRequests:
		return KindRateLimited
	default:
		return KindUpstream
	}
}

func upstreamErr(op string, schema Schema, status int) error {
	return &OpError{Op: op, Schema: schema, Kind: statusKind(status), Status: status}
}
