package rousi

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (*Adapter) Schema() adapter.Schema { return adapter.SchemaRousi }

func (*Adapter) DisplayName() string { return "Rousi" }

func (*Adapter) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{
		Login:      true,
		APIKeyAuth: true,
		Signin:     true,
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

func (a *Adapter) Stats(ctx context.Context, s *adapter.Session) (*adapter.UserStats, error) {
	root, err := fetchProfile(ctx, s)
	if err != nil {
		return nil, err
	}
	return parseProfile(root, s.Site.Timezone)
}

func (a *Adapter) Signin(ctx context.Context, s *adapter.Session) (*adapter.SigninResult, error) {
	if strings.TrimSpace(s.Secret.APIKey) == "" {
		return &adapter.SigninResult{
			Outcome: adapter.OutcomeAuthFailed,
			Reason:  "no_credential",
			Message: "No API key is stored for this site.",
		}, nil
	}

	base, err := origin(s)
	if err != nil {
		return nil, err
	}
	endpoint := base + "/api/points/attendance"
	page, err := s.Client.PostJSON(ctx, endpoint, []byte("{}"), authHeader(s))
	if err != nil {
		return &adapter.SigninResult{Outcome: adapter.OutcomeNetworkError, Reason: "network", Message: "The site could not be reached."}, nil
	}

	var root map[string]any
	if decodeErr := adapter.DecodeJSON(page.Body, &root); decodeErr != nil {
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeParseError,
			Reason:     "parse",
			Message:    "The sign-in response could not be read.",
			HTTPStatus: page.Status,
		}, nil
	}

	message := adapter.JSONString(root["message"])
	code, hasCode := adapter.JSONInt(root["code"])
	switch {
	case hasCode && *code == 0:
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeSuccess,
			Reason:     "confirmed",
			Message:    firstNonEmpty(message, "The site confirmed the sign-in."),
			Evidence:   adapter.TruncateEvidence(message, 160),
			HTTPStatus: page.Status,
		}, nil
	case strings.Contains(message, "已签") || strings.Contains(message, "已簽"):
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeAlreadySigned,
			Reason:     "already_signed",
			Message:    firstNonEmpty(message, "The site reports the sign-in for today is already done."),
			Evidence:   adapter.TruncateEvidence(message, 160),
			HTTPStatus: page.Status,
		}, nil
	default:
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeAuthFailed,
			Reason:     "site_refused",
			Message:    firstNonEmpty(message, "The site refused the sign-in."),
			Evidence:   adapter.TruncateEvidence(message, 160),
			HTTPStatus: page.Status,
		}, nil
	}
}

func check(ctx context.Context, s *adapter.Session) (*adapter.LoginResult, error) {
	if strings.TrimSpace(s.Secret.APIKey) == "" {
		return &adapter.LoginResult{}, &adapter.OpError{
			Op: "check_session", Schema: adapter.SchemaRousi, Kind: adapter.KindConfig,
			Err: errors.New("no API key stored"),
		}
	}
	root, err := fetchProfile(ctx, s)
	if err != nil {
		return nil, err
	}
	result := &adapter.LoginResult{LoggedIn: true}
	if name, ok := adapter.Map(root, "data", "username"); ok {
		result.Username = adapter.JSONString(name)
	}
	return result, nil
}

func fetchProfile(ctx context.Context, s *adapter.Session) (map[string]any, error) {
	base, err := origin(s)
	if err != nil {
		return nil, err
	}
	page, err := s.Client.Get(ctx, base+"/api/v1/profile", authHeader(s))
	if err != nil {
		return nil, err
	}
	switch {
	case page.Status == 401 || page.Status == 403:
		return nil, &adapter.OpError{Op: "profile", Schema: adapter.SchemaRousi, Kind: adapter.KindAuth, Status: page.Status}
	case page.Status < 200 || page.Status >= 300:
		return nil, &adapter.OpError{Op: "profile", Schema: adapter.SchemaRousi, Kind: adapter.KindUpstream, Status: page.Status}
	}

	var root map[string]any
	if err := adapter.DecodeJSON(page.Body, &root); err != nil {
		return nil, &adapter.OpError{Op: "profile", Schema: adapter.SchemaRousi, Kind: adapter.KindParse, Err: err}
	}
	if code, ok := adapter.JSONInt(root["code"]); !ok || *code != 0 {
		msg := adapter.JSONString(root["message"])
		if msg == "" {
			msg = "the site returned a non-zero status"
		}
		return nil, &adapter.OpError{Op: "profile", Schema: adapter.SchemaRousi, Kind: adapter.KindAuth, Err: errors.New(msg)}
	}
	return root, nil
}

func parseProfile(root map[string]any, loc *time.Location) (*adapter.UserStats, error) {
	data, _ := adapter.Map(root, "data")
	obj, ok := data.(map[string]any)
	if !ok {
		return nil, &adapter.OpError{Op: "stats", Schema: adapter.SchemaRousi, Kind: adapter.KindParse, Err: errors.New("profile data is not an object")}
	}

	out := &adapter.UserStats{
		Username:  adapter.JSONString(obj["username"]),
		UserLevel: adapter.JSONString(obj["level_text"]),
		Extras:    map[string]any{},
	}
	out.UploadedBytes, _ = adapter.JSONInt64(obj["uploaded"])
	out.DownloadedBytes, _ = adapter.JSONInt64(obj["downloaded"])
	out.Ratio, _ = adapter.JSONNumber(obj["ratio"])
	out.Bonus, _ = adapter.JSONNumber(obj["karma"])
	out.JoinAt = adapter.JSONTime(obj["registered_at"], loc)

	if seeding, ok := adapter.Map(obj, "seeding_leeching_data"); ok {
		if seedingObj, ok := seeding.(map[string]any); ok {
			out.Seeding, _ = adapter.JSONInt(seedingObj["seeding_count"])
			out.SeedingSizeBytes, _ = adapter.JSONInt64(seedingObj["seeding_size"])
			out.Leeching, _ = adapter.JSONInt(seedingObj["leeching_count"])
			out.LeechingSizeBytes, _ = adapter.JSONInt64(seedingObj["leeching_size"])
		}
	}

	if out.UploadedBytes == nil || out.DownloadedBytes == nil {
		return nil, &adapter.OpError{Op: "stats", Schema: adapter.SchemaRousi, Kind: adapter.KindParse, Err: errors.New("upload and download missing from profile")}
	}
	return out, nil
}

func origin(s *adapter.Session) (string, error) {
	raw := s.Site.APIBaseURL
	if strings.TrimSpace(raw) == "" {
		raw = s.Site.BaseURL
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", &adapter.OpError{Op: "api", Schema: adapter.SchemaRousi, Kind: adapter.KindConfig, Err: errors.New("site URL is not absolute")}
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func authHeader(s *adapter.Session) map[string]string {
	return map[string]string{"Authorization": "Bearer " + s.Secret.APIKey}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
