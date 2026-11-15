// Package mteam implements the M-Team dialect. M-Team exposes a read-only
// account API behind an API key; it has no public sign-in endpoint, so the
// plugin collects statistics and refuses to pretend it signed in.
package mteam

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

// Adapter is the M-Team dialect.
type Adapter struct{}

// New returns the dialect.
func New() *Adapter { return &Adapter{} }

func (*Adapter) Schema() adapter.Schema { return adapter.SchemaMTeam }

func (*Adapter) DisplayName() string { return "M-Team" }

func (*Adapter) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{
		Login:      true,
		APIKeyAuth: true,
		Signin:     false,
		SigninNote: "This site has no public sign-in endpoint. The plugin reads data only.",
		Stats:      true,
		Bonus:      true,
	}
}

func (*Adapter) Defaults() adapter.Defaults {
	return adapter.Defaults{TimeoutSeconds: 20, LimitIntervalSeconds: 5}
}

func (a *Adapter) Login(ctx context.Context, s *adapter.Session) (*adapter.LoginResult, error) {
	return check(ctx, s)
}

func (a *Adapter) CheckSession(ctx context.Context, s *adapter.Session) (*adapter.LoginResult, error) {
	return check(ctx, s)
}

// Signin reports the missing capability instead of inventing a request. The
// site's own "sign-in" is a signed last-browse call whose secret is scraped
// from front-end code and rotates without notice.
func (*Adapter) Signin(context.Context, *adapter.Session) (*adapter.SigninResult, error) {
	return &adapter.SigninResult{
		Outcome: adapter.OutcomeUnsupported,
		Reason:  "signin_unsupported",
		Message: "This site has no public sign-in endpoint.",
	}, nil
}

func (a *Adapter) Stats(ctx context.Context, s *adapter.Session) (*adapter.UserStats, error) {
	profile, err := fetchProfile(ctx, s)
	if err != nil {
		return nil, err
	}
	return parseProfile(profile, s.Site.Timezone)
}

func check(ctx context.Context, s *adapter.Session) (*adapter.LoginResult, error) {
	if strings.TrimSpace(s.Secret.APIKey) == "" {
		return &adapter.LoginResult{}, &adapter.OpError{
			Op: "check_session", Schema: adapter.SchemaMTeam, Kind: adapter.KindConfig,
			Err: errors.New("no API key stored"),
		}
	}
	profile, err := fetchProfile(ctx, s)
	if err != nil {
		return nil, err
	}
	result := &adapter.LoginResult{LoggedIn: true}
	if id, ok := adapter.Map(profile, "data", "id"); ok {
		result.UserID = fmt.Sprint(id)
	}
	if name, ok := adapter.Map(profile, "data", "username"); ok {
		result.Username = adapter.JSONString(name)
	}
	return result, nil
}

// apiBase resolves the API host. M-Team serves the site at kp.m-team.cc and the
// API at api.m-team.cc, so the host cannot simply be reused.
func apiBase(s *adapter.Session) (string, error) {
	if raw := strings.TrimSpace(s.Site.APIBaseURL); raw != "" {
		return trimSlash(raw), nil
	}
	parsed, err := url.Parse(s.Site.BaseURL)
	if err != nil || parsed.Host == "" {
		return "", &adapter.OpError{Op: "api", Schema: adapter.SchemaMTeam, Kind: adapter.KindConfig, Err: errors.New("site URL is not absolute")}
	}
	host := parsed.Host
	if strings.HasPrefix(host, "kp.") {
		return "https://api." + strings.TrimPrefix(host, "kp."), nil
	}
	return "https://api." + host, nil
}

func fetchProfile(ctx context.Context, s *adapter.Session) (map[string]any, error) {
	base, err := apiBase(s)
	if err != nil {
		return nil, err
	}
	hdr := map[string]string{"x-api-key": s.Secret.APIKey}
	if s.Secret.Token != "" {
		// Some deployments put the browser token here; the key remains primary.
		hdr["Authorization"] = s.Secret.Token
	}

	page, err := s.Client.Get(ctx, base+"/api/member/profile", hdr)
	if err != nil {
		return nil, err
	}
	if page.Status == 401 || page.Status == 403 {
		return nil, &adapter.OpError{Op: "profile", Schema: adapter.SchemaMTeam, Kind: adapter.KindAuth, Status: page.Status}
	}
	if page.Status < 200 || page.Status >= 300 {
		return nil, &adapter.OpError{Op: "profile", Schema: adapter.SchemaMTeam, Kind: adapter.KindUpstream, Status: page.Status}
	}

	var root map[string]any
	if err := adapter.DecodeJSON(page.Body, &root); err != nil {
		return nil, &adapter.OpError{Op: "profile", Schema: adapter.SchemaMTeam, Kind: adapter.KindParse, Err: err}
	}
	if msg := adapter.JSONString(root["message"]); msg != "" && !strings.EqualFold(msg, "SUCCESS") {
		return nil, &adapter.OpError{Op: "profile", Schema: adapter.SchemaMTeam, Kind: adapter.KindAuth, Err: fmt.Errorf("site says %q", msg)}
	}
	if _, ok := adapter.Map(root, "data"); !ok {
		return nil, &adapter.OpError{Op: "profile", Schema: adapter.SchemaMTeam, Kind: adapter.KindAuth, Err: errors.New("no data in profile response")}
	}
	return root, nil
}

func parseProfile(root map[string]any, loc *time.Location) (*adapter.UserStats, error) {
	data, _ := adapter.Map(root, "data")
	obj, ok := data.(map[string]any)
	if !ok {
		return nil, &adapter.OpError{Op: "stats", Schema: adapter.SchemaMTeam, Kind: adapter.KindParse, Err: errors.New("profile data is not an object")}
	}

	out := &adapter.UserStats{
		Username: adapter.JSONString(obj["username"]),
		Extras:   map[string]any{},
	}
	if id, ok := obj["id"]; ok {
		out.UserID = fmt.Sprint(id)
	}
	if role, ok := adapter.JSONInt(obj["role"]); ok {
		out.Extras["role_id"] = *role
		out.UserLevel = levelName(*role)
	}
	if created, ok := adapter.Map(obj, "memberStatus", "createdDate"); ok {
		out.JoinAt = adapter.JSONTime(created, loc)
	}

	if counts, ok := adapter.Map(obj, "memberCount"); ok {
		if countsObj, ok := counts.(map[string]any); ok {
			out.UploadedBytes, _ = adapter.JSONInt64(countsObj["uploaded"])
			out.DownloadedBytes, _ = adapter.JSONInt64(countsObj["downloaded"])
			out.Ratio, _ = adapter.JSONNumber(countsObj["shareRate"])
			out.Bonus, _ = adapter.JSONNumber(countsObj["bonus"])
			out.Seeding, _ = adapter.JSONInt(countsObj["seeding"])
			out.Leeching, _ = adapter.JSONInt(countsObj["leeching"])
		}
	}

	if out.UploadedBytes == nil || out.DownloadedBytes == nil {
		return nil, &adapter.OpError{Op: "stats", Schema: adapter.SchemaMTeam, Kind: adapter.KindParse, Err: errors.New("upload and download missing from profile")}
	}
	return out, nil
}

// levelName maps M-Team's numeric role to a label. Unknown roles are reported
// as "Role N" rather than guessed.
func levelName(role int) string {
	switch role {
	case 1:
		return "User"
	case 2:
		return "Power User"
	case 3:
		return "Elite User"
	case 4:
		return "Crazy User"
	case 5:
		return "Insane User"
	case 6:
		return "Veteran User"
	case 7:
		return "Extreme User"
	case 8:
		return "Ultimate User"
	case 9:
		return "Nexus Master"
	case 10:
		return "VIP"
	case 11:
		return "Uploader"
	case 12:
		return "Moderator"
	case 13:
		return "Administrator"
	case 14:
		return "Sysop"
	case 15:
		return "Staff"
	}
	return fmt.Sprintf("Role %d", role)
}

func trimSlash(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func profileUserAgent(s *adapter.Session) string { return s.Site.UserAgent }
