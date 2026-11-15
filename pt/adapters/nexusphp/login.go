package nexusphp

import (
	"context"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

// loginForm is the scraped login form: where to post and what to send besides
// the account fields.
type loginForm struct {
	action string
	hidden url.Values
}

// login authenticates with the stored account and secret through the site's
// own login form.
//
// Business outcomes (captcha, rejected credential) are reported in the result;
// only transport and template faults come back as errors. That keeps the
// service free of string matching.
func login(ctx context.Context, s *adapter.Session) (*adapter.LoginResult, error) {
	if strings.TrimSpace(s.Site.Username) == "" || s.Secret.Login == "" {
		return &adapter.LoginResult{LoggedIn: false}, &adapter.OpError{
			Op: "login", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindConfig,
			Err: errNoLoginCredential,
		}
	}

	p, err := page(ctx, s, "/login.php")
	if err != nil {
		return nil, err
	}
	doc, err := parseDoc(p.Body)
	if err != nil {
		return nil, &adapter.OpError{Op: "login", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindParse, Err: err}
	}

	if detectCaptcha(doc, p.Body) {
		return &adapter.LoginResult{ManualReason: "captcha"}, nil
	}

	form, ok := findLoginForm(doc, p.URL.String())
	if !ok {
		return nil, &adapter.OpError{Op: "login", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindParse, Err: errNoLoginForm}
	}

	values := url.Values{}
	for k, vv := range form.hidden {
		values[k] = vv
	}
	values.Set(loginFieldName, s.Site.Username)
	values.Set(loginFieldPass, s.Secret.Login)

	// A successful form login answers 302 with Set-Cookie; a client that
	// follows redirects hides that response.
	resp, err := s.Client.NoRedirect().PostForm(ctx, form.action, values, p.URL.String())
	if err != nil {
		return nil, err
	}
	if resp.Status >= 500 {
		return nil, &adapter.OpError{Op: "login", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindUpstream, Status: resp.Status}
	}

	// NexusPHP issues these two cookies on success. Their absence is not proof
	// of failure (skins differ), so the session check below has the last word.
	gotSessionCookie := s.Client.HasCookie("c_secure_uid") && s.Client.HasCookie("c_secure_pass")

	result, err := checkSession(ctx, s)
	if err != nil {
		return nil, err
	}
	if result.LoggedIn {
		result.Cookie = s.Client.Cookies()
		return result, nil
	}

	// The POST answered, but the session is still anonymous. Look at the body
	// for the site's own error text before reporting a plain auth failure.
	if msg := loginErrorText(resp.Body); msg != "" {
		return &adapter.LoginResult{LoggedIn: false, ManualReason: ""}, &adapter.OpError{
			Op: "login", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindAuth, Status: resp.Status,
			Err: errLoginRejected(msg),
		}
	}
	if !gotSessionCookie {
		return &adapter.LoginResult{LoggedIn: false}, &adapter.OpError{
			Op: "login", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindAuth, Status: resp.Status,
			Err: errLoginRejected(""),
		}
	}
	return &adapter.LoginResult{LoggedIn: false}, nil
}

// findLoginForm locates the form carrying the login fields and captures its
// hidden inputs (CSRF tokens, "log in" flags, return paths).
func findLoginForm(doc *goquery.Document, pageURL string) (loginForm, bool) {
	var out loginForm
	if doc == nil {
		return out, false
	}

	var matched bool
	doc.Find("form").EachWithBreak(func(_ int, form *goquery.Selection) bool {
		hasName, hasPass := false, false
		form.Find("input").Each(func(_ int, in *goquery.Selection) {
			name, _ := in.Attr("name")
			typ, _ := in.Attr("type")
			switch {
			case name == loginFieldName:
				hasName = true
			case name == loginFieldPass || strings.EqualFold(typ, loginFieldPass):
				hasPass = true
			}
		})
		if !hasName && !hasPass {
			return true
		}

		action, _ := form.Attr("action")
		out.action = resolveAction(pageURL, action)
		out.hidden = url.Values{}
		form.Find("input").Each(func(_ int, in *goquery.Selection) {
			name, _ := in.Attr("name")
			if name == "" || name == loginFieldName || name == loginFieldPass {
				return
			}
			typ, _ := in.Attr("type")
			if strings.EqualFold(typ, "submit") || strings.EqualFold(typ, "button") || strings.EqualFold(typ, "image") {
				return
			}
			value, _ := in.Attr("value")
			out.hidden.Add(name, value)
		})
		matched = true
		return false
	})
	return out, matched
}

// resolveAction makes a form action absolute, defaulting to takelogin.php.
func resolveAction(pageURL, action string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		return "/takelogin.php"
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return action
	}
	ref, err := url.Parse(action)
	if err != nil {
		return action
	}
	return base.ResolveReference(ref).String()
}

// checkSession reports whether the current cookies still authenticate.
func checkSession(ctx context.Context, s *adapter.Session) (*adapter.LoginResult, error) {
	p, err := page(ctx, s, "/index.php")
	if err != nil {
		return nil, err
	}
	doc, err := parseDoc(p.Body)
	if err != nil {
		return nil, &adapter.OpError{Op: "check_session", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindParse, Err: err}
	}
	result := &adapter.LoginResult{}
	if loggedIn(doc) {
		result.LoggedIn = true
		result.UserID = userIDFromHTML(p.Body)
		result.Username = usernameFromDoc(doc)
	}
	return result, nil
}

// loginErrorText pulls the site's own rejection message out of a login
// response, so the ledger shows why instead of a generic failure.
func loginErrorText(body []byte) string {
	doc, err := parseDoc(body)
	if err != nil {
		return ""
	}
	text := visibleText(doc)
	for _, marker := range []string{"用户名或密码错误", "使用者名稱或密碼錯誤", "密码错误", "密碼錯誤", "登录失败", "登錄失敗", "账号密码错误"} {
		if idx := strings.Index(text, marker); idx >= 0 {
			return adapter.TruncateEvidence(text[idx:], 120)
		}
	}
	return ""
}
