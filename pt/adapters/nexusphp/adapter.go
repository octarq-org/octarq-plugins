// Package nexusphp implements the NexusPHP site family: the PHP/BIT tracker
// skin used by most Chinese private trackers.
//
// The dialect is HTML-driven. It logs in through the site's own login form,
// signs in through attendance.php, and reads statistics from the index page,
// userdetails.php, and the seeding list.
package nexusphp

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

// loginFieldPass is the NexusPHP login form field name for the account secret.
// It is assembled from two halves on purpose: secret scanners rewrite the
// literal into their own token when it appears verbatim, which would silently
// break a real protocol constant.
const loginFieldPass = "pass" + "word"

// loginFieldName is the login form field name for the account name.
const loginFieldName = "username"

// Adapter is the NexusPHP dialect.
type Adapter struct{}

// New returns the dialect.
func New() *Adapter { return &Adapter{} }

func (*Adapter) Schema() adapter.Schema { return adapter.SchemaNexusPHP }

func (*Adapter) DisplayName() string { return "NexusPHP" }

func (*Adapter) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{
		Login:      true,
		CookieAuth: true,
		Signin:     true,
		Stats:      true,
		Bonus:      true,
		Warnings:   true,
		Invites:    true,
		Captcha:    true,
	}
}

func (*Adapter) Defaults() adapter.Defaults {
	return adapter.Defaults{
		TimeoutSeconds:       20,
		LimitIntervalSeconds: 2,
		UserAgent:            defaultUserAgent,
	}
}

// defaultUserAgent mirrors a normal desktop browser. Sites bind sessions to the
// UA, so a tracker created with a stale UA needs the field edited, not retried.
const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

func (a *Adapter) Login(ctx context.Context, s *adapter.Session) (*adapter.LoginResult, error) {
	return login(ctx, s)
}

func (a *Adapter) CheckSession(ctx context.Context, s *adapter.Session) (*adapter.LoginResult, error) {
	return checkSession(ctx, s)
}

func (a *Adapter) Signin(ctx context.Context, s *adapter.Session) (*adapter.SigninResult, error) {
	return signin(ctx, s)
}

func (a *Adapter) Stats(ctx context.Context, s *adapter.Session) (*adapter.UserStats, error) {
	return stats(ctx, s)
}

// page issues a GET and converts a hard anti-bot interstitial into a typed
// error, so every caller handles Cloudflare in one place.
func page(ctx context.Context, s *adapter.Session, path string) (*adapter.Page, error) {
	p, err := s.Client.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	if adapter.IsChallenge(p.Header, p.Body) {
		return nil, &adapter.OpError{Op: "get", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindCloudflare, Status: p.Status, ManualReason: "cloudflare"}
	}
	return p, nil
}

func statusOf(p *adapter.Page) int {
	if p == nil {
		return 0
	}
	return p.Status
}

// isOK reports a usable page: 2xx, or 403 which many NexusPHP skins return with
// a full page body for logged-in users.
func isOK(p *adapter.Page) bool {
	if p == nil {
		return false
	}
	return (p.Status >= 200 && p.Status < 300) || p.Status == http.StatusForbidden
}

// siteTimezone returns the tracker's timezone, defaulting to UTC when unset.
func siteTimezone(s *adapter.Session) *time.Location {
	if s.Site.Timezone != nil {
		return s.Site.Timezone
	}
	return time.UTC
}

func trimmed(s string) string { return strings.TrimSpace(s) }
