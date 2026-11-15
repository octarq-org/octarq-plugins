// Package hddolby implements the HDDolby dialect: a read-only account API
// behind an API key.
package hddolby

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

// Adapter is the HDDolby dialect.
type Adapter struct{}

// New returns the dialect.
func New() *Adapter { return &Adapter{} }

func (*Adapter) Schema() adapter.Schema { return adapter.SchemaHDDolby }

func (*Adapter) DisplayName() string { return "HDDolby" }

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

func (*Adapter) Signin(context.Context, *adapter.Session) (*adapter.SigninResult, error) {
	return &adapter.SigninResult{
		Outcome: adapter.OutcomeUnsupported,
		Reason:  "signin_unsupported",
		Message: "This site has no public sign-in endpoint.",
	}, nil
}

func (a *Adapter) Stats(ctx context.Context, s *adapter.Session) (*adapter.UserStats, error) {
	record, err := fetchUserData(ctx, s)
	if err != nil {
		return nil, err
	}
	return parseUserData(record, s.Site.Timezone)
}

func check(ctx context.Context, s *adapter.Session) (*adapter.LoginResult, error) {
	if strings.TrimSpace(s.Secret.APIKey) == "" {
		return &adapter.LoginResult{}, &adapter.OpError{
			Op: "check_session", Schema: adapter.SchemaHDDolby, Kind: adapter.KindConfig,
			Err: errors.New("no API key stored"),
		}
	}
	if _, err := fetchUserData(ctx, s); err != nil {
		return nil, err
	}
	return &adapter.LoginResult{LoggedIn: true}, nil
}

func fetchUserData(ctx context.Context, s *adapter.Session) (map[string]any, error) {
	endpoint := strings.TrimRight(s.Site.BaseURL, "/") + "/api/v1/user/data"
	page, err := s.Client.Get(ctx, endpoint, map[string]string{"x-api-key": s.Secret.APIKey})
	if err != nil {
		return nil, err
	}
	switch {
	case page.Status == 401 || page.Status == 403:
		return nil, &adapter.OpError{Op: "user_data", Schema: adapter.SchemaHDDolby, Kind: adapter.KindAuth, Status: page.Status}
	case page.Status < 200 || page.Status >= 300:
		return nil, &adapter.OpError{Op: "user_data", Schema: adapter.SchemaHDDolby, Kind: adapter.KindUpstream, Status: page.Status}
	}

	var root map[string]any
	if err := adapter.DecodeJSON(page.Body, &root); err != nil {
		return nil, &adapter.OpError{Op: "user_data", Schema: adapter.SchemaHDDolby, Kind: adapter.KindParse, Err: err}
	}
	if status, ok := adapter.JSONInt(root["status"]); !ok || *status != 0 {
		msg := adapter.JSONString(root["msg"])
		if msg == "" {
			msg = "the site returned a non-zero status"
		}
		return nil, &adapter.OpError{Op: "user_data", Schema: adapter.SchemaHDDolby, Kind: adapter.KindAuth, Err: errors.New(msg)}
	}

	data, ok := root["data"]
	if !ok {
		return nil, &adapter.OpError{Op: "user_data", Schema: adapter.SchemaHDDolby, Kind: adapter.KindParse, Err: errors.New("no data in response")}
	}
	// The API wraps the single record in a one-element list.
	if record, ok := adapter.FirstMap(data); ok {
		return record, nil
	}
	if obj, ok := data.(map[string]any); ok {
		return obj, nil
	}
	return nil, &adapter.OpError{Op: "user_data", Schema: adapter.SchemaHDDolby, Kind: adapter.KindParse, Err: errors.New("unexpected data shape")}
}

func parseUserData(record map[string]any, loc *time.Location) (*adapter.UserStats, error) {
	out := &adapter.UserStats{Extras: map[string]any{}}

	out.UploadedBytes, _ = adapter.JSONInt64(record["uploaded"])
	out.DownloadedBytes, _ = adapter.JSONInt64(record["downloaded"])
	out.Bonus, _ = adapter.JSONNumber(record["seedbonus"])
	out.UnreadMessages, _ = adapter.JSONInt(record["unread_messages"])
	out.JoinAt = adapter.JSONTime(record["added"], loc)

	if class, ok := adapter.JSONInt(record["class"]); ok {
		out.Extras["class_id"] = *class
		out.UserLevel = fmt.Sprintf("Class %d", *class)
	}

	if out.UploadedBytes == nil || out.DownloadedBytes == nil {
		return nil, &adapter.OpError{Op: "stats", Schema: adapter.SchemaHDDolby, Kind: adapter.KindParse, Err: errors.New("upload and download missing from response")}
	}
	return out, nil
}
