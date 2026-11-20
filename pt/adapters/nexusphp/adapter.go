package nexusphp

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

const loginFieldPass = "pass" + "word"

const loginFieldName = "username"

type Adapter struct{}

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

func isOK(p *adapter.Page) bool {
	if p == nil {
		return false
	}
	return (p.Status >= 200 && p.Status < 300) || p.Status == http.StatusForbidden
}

func siteTimezone(s *adapter.Session) *time.Location {
	if s.Site.Timezone != nil {
		return s.Site.Timezone
	}
	return time.UTC
}

func trimmed(s string) string { return strings.TrimSpace(s) }
