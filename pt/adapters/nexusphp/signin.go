package nexusphp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

var (
	alreadyMarkers = []string{
		"今天已签到", "今日已签到", "今天已簽到", "今日已簽到",
		"已经签到", "已签到", "已經簽到", "已簽到", "已打卡",
		"You have already attend",
	}
	successMarkers = []string{
		"签到成功", "簽到成功", "签到已得", "簽到已得",
		"本次签到获得", "本次簽到獲得", "今天签到您获得",
	}
	negativeMarkers = []string{
		"签到失败", "簽到失敗", "签到失敗", "簽到失败",
		"没有签到成功", "沒有簽到成功", "未成功",
		"请先登录", "請先登入", "请先登入", "未登录", "未登入",
		"无权", "無權", "禁止", "账号被禁", "帳號被禁",
	}
	pendingMarkers = []string{"立即签到", "立即簽到", "每日签到", "每日簽到", "点击签到", "點擊簽到", "打卡", "签到"}
)

var bonusRe = regexp.MustCompile(`(?:本次签到获得|本次簽到獲得|签到已得|簽到已得|今天签到您获得)[^\d\-]{0,20}([\d,]+(?:\.\d+)?)`)

func signin(ctx context.Context, s *adapter.Session) (*adapter.SigninResult, error) {
	p, err := page(ctx, s, "/attendance.php")
	if err != nil {
		return resultFromError(err), nil
	}
	if !isOK(p) {
		if p.Status == http.StatusNotFound || p.Status == http.StatusGone {
			return signinViaIndex(ctx, s)
		}
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeNetworkError,
			Reason:     "http_status",
			Message:    "Attendance page returned an unexpected status.",
			HTTPStatus: p.Status,
		}, nil
	}

	doc, docErr := parseDoc(p.Body)
	if docErr != nil {
		return &adapter.SigninResult{Outcome: adapter.OutcomeParseError, Reason: "parse", Message: "Attendance page could not be parsed.", HTTPStatus: p.Status}, nil
	}

	if hasLoginForm(doc) {
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeAuthFailed,
			Reason:     "cookie_expired",
			Message:    "The site answered with a login form. The saved session expired.",
			HTTPStatus: p.Status,
		}, nil
	}

	verdict := evaluateSignin(doc, p.Body, p.Status)
	if verdict.Outcome != adapter.OutcomeLoginOKUnconfirmed {
		return verdict, nil
	}

	form, ok := findSigninForm(doc, p.URL.String())
	if !ok {
		return verdict, nil
	}
	resp, err := s.Client.PostForm(ctx, form.action, form.values, p.URL.String())
	if err != nil {
		return resultFromError(err), nil
	}
	doc2, err := parseDoc(resp.Body)
	if err != nil {
		return &adapter.SigninResult{Outcome: adapter.OutcomeParseError, Reason: "parse", Message: "Sign-in response could not be parsed.", HTTPStatus: resp.Status}, nil
	}
	posted := evaluateSignin(doc2, resp.Body, resp.Status)
	if posted.Outcome == adapter.OutcomeLoginOKUnconfirmed {
		posted.Message = "The sign-in request was sent, but the site did not confirm the result."
	}
	return posted, nil
}

func signinViaIndex(ctx context.Context, s *adapter.Session) (*adapter.SigninResult, error) {
	p, err := page(ctx, s, "/index.php")
	if err != nil {
		return resultFromError(err), nil
	}
	doc, docErr := parseDoc(p.Body)
	if docErr != nil {
		return &adapter.SigninResult{Outcome: adapter.OutcomeParseError, Reason: "parse", Message: "The index page could not be parsed.", HTTPStatus: p.Status}, nil
	}
	if hasLoginForm(doc) {
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeAuthFailed,
			Reason:     "cookie_expired",
			Message:    "The site answered with a login form. The saved session expired.",
			HTTPStatus: p.Status,
		}, nil
	}

	verdict := evaluateSignin(doc, p.Body, p.Status)
	if verdict.Outcome != adapter.OutcomeLoginOKUnconfirmed {
		return verdict, nil
	}

	target, ok := findSigninHref(doc, p.URL.String())
	if !ok {
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeUnsupported,
			Reason:     "no_signin_endpoint",
			Message:    "This site has no daily sign-in.",
			HTTPStatus: p.Status,
		}, nil
	}

	resp, err := page(ctx, s, target)
	if err != nil {
		return resultFromError(err), nil
	}
	if !isOK(resp) {
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeNetworkError,
			Reason:     "http_status",
			Message:    "The sign-in page returned an unexpected status.",
			HTTPStatus: resp.Status,
		}, nil
	}
	signinDoc, signinErr := parseDoc(resp.Body)
	if signinErr != nil {
		return &adapter.SigninResult{Outcome: adapter.OutcomeParseError, Reason: "parse", Message: "The sign-in response could not be parsed.", HTTPStatus: resp.Status}, nil
	}
	posted := evaluateSignin(signinDoc, resp.Body, resp.Status)
	if posted.Outcome != adapter.OutcomeLoginOKUnconfirmed {
		return posted, nil
	}

	fresh, err := page(ctx, s, "/index.php")
	if err != nil {
		return resultFromError(err), nil
	}
	freshDoc, freshErr := parseDoc(fresh.Body)
	if freshErr != nil {
		return &adapter.SigninResult{Outcome: adapter.OutcomeParseError, Reason: "parse", Message: "The index page could not be parsed.", HTTPStatus: fresh.Status}, nil
	}
	if hasLoginForm(freshDoc) {
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeAuthFailed,
			Reason:     "cookie_expired",
			Message:    "The site answered with a login form. The saved session expired.",
			HTTPStatus: fresh.Status,
		}, nil
	}
	freshVerdict := evaluateSignin(freshDoc, fresh.Body, fresh.Status)
	switch freshVerdict.Outcome {
	case adapter.OutcomeSuccess, adapter.OutcomeAlreadySigned, adapter.OutcomeAuthFailed:
		return freshVerdict, nil
	}
	if _, still := findSigninHref(freshDoc, fresh.URL.String()); !still {
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeAlreadySigned,
			Reason:     "already_signed",
			Message:    "The site reports the sign-in for today is already done.",
			Evidence:   freshVerdict.Evidence,
			HTTPStatus: fresh.Status,
		}, nil
	}
	return &adapter.SigninResult{
		Outcome:    adapter.OutcomeLoginOKUnconfirmed,
		Reason:     "confirmed_pending",
		Message:    "The sign-in request was sent, but the site did not confirm the result.",
		Evidence:   freshVerdict.Evidence,
		HTTPStatus: fresh.Status,
	}, nil
}

func findSigninHref(doc *goquery.Document, pageURL string) (string, bool) {
	if doc == nil {
		return "", false
	}
	var out string
	doc.Find("a").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		href, _ := sel.Attr("href")
		if href == "" {
			return true
		}
		if strings.Contains(href, "action=addbonus") || containsAny(sel.Text(), "签到", "簽到", "打卡") {
			out = resolveAction(pageURL, href)
			return false
		}
		return true
	})
	return out, out != ""
}

type signinForm struct {
	action string
	values url.Values
}

func findSigninForm(doc *goquery.Document, pageURL string) (signinForm, bool) {
	var out signinForm
	if doc == nil {
		return out, false
	}
	var matched bool
	doc.Find("form").EachWithBreak(func(_ int, form *goquery.Selection) bool {
		action, _ := form.Attr("action")
		signals := []string{action, form.Text()}
		form.Find("input, button").Each(func(_ int, in *goquery.Selection) {
			if v, ok := in.Attr("value"); ok {
				signals = append(signals, v)
			}
			signals = append(signals, in.Text())
		})
		joined := strings.Join(signals, " ")
		if !containsAny(joined, "签到", "簽到", "打卡", "attendance", "signin", "showup") {
			return true
		}
		out.action = resolveAction(pageURL, action)
		out.values = url.Values{}
		form.Find("input").Each(func(_ int, in *goquery.Selection) {
			name, _ := in.Attr("name")
			if name == "" {
				return
			}
			typ, _ := in.Attr("type")
			if strings.EqualFold(typ, "submit") || strings.EqualFold(typ, "button") {
				out.values.Set(name, "1")
				return
			}
			value, _ := in.Attr("value")
			out.values.Add(name, value)
		})
		form.Find("button").Each(func(_ int, in *goquery.Selection) {
			if name, ok := in.Attr("name"); ok && name != "" {
				out.values.Set(name, "1")
			}
		})
		matched = true
		return false
	})
	return out, matched
}

func evaluateSignin(doc *goquery.Document, body []byte, status int) *adapter.SigninResult {
	text := visibleText(doc)
	evidence := adapter.TruncateEvidence(text, 160)

	if containsAny(text, alreadyMarkers...) {
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeAlreadySigned,
			Reason:     "already_signed",
			Message:    "The site reports the sign-in for today is already done.",
			Evidence:   evidence,
			HTTPStatus: status,
		}
	}
	if containsAny(text, negativeMarkers...) {
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeAuthFailed,
			Reason:     "site_refused",
			Message:    "The site refused the request. Check the account state on the site.",
			Evidence:   evidence,
			HTTPStatus: status,
		}
	}
	if containsAny(text, successMarkers...) {
		return &adapter.SigninResult{
			Outcome:      adapter.OutcomeSuccess,
			Reason:       "confirmed",
			Message:      "The site confirmed the sign-in.",
			Evidence:     evidence,
			BonusAwarded: bonusFromText(text),
			HTTPStatus:   status,
		}
	}
	if containsAny(text, pendingMarkers...) {
		return &adapter.SigninResult{
			Outcome:    adapter.OutcomeLoginOKUnconfirmed,
			Reason:     "form_pending",
			Message:    "The sign-in form is still on the page.",
			Evidence:   evidence,
			HTTPStatus: status,
		}
	}
	return &adapter.SigninResult{
		Outcome:    adapter.OutcomeLoginOKUnconfirmed,
		Reason:     "no_evidence",
		Message:    "The session works, but the page carried no sign-in result.",
		Evidence:   evidence,
		HTTPStatus: status,
	}
}

func bonusFromText(text string) *float64 {
	m := bonusRe.FindStringSubmatch(text)
	if len(m) != 2 {
		return nil
	}
	bonus, ok := parseBonus(m[1])
	if !ok {
		return nil
	}
	return bonus
}

func resultFromError(err error) *adapter.SigninResult {
	switch {
	case errors.Is(err, adapter.ErrCloudflare):
		return &adapter.SigninResult{
			Outcome: adapter.OutcomeNeedsManualAction,
			Reason:  "cloudflare",
			Message: "The site is behind an anti-bot challenge. Paste a browser cookie to continue.",
		}
	case errors.Is(err, adapter.ErrCaptcha):
		return &adapter.SigninResult{
			Outcome: adapter.OutcomeNeedsManualAction,
			Reason:  "captcha",
			Message: "The site requires a captcha. Paste a browser cookie to continue.",
		}
	case errors.Is(err, adapter.ErrRateLimited):
		return &adapter.SigninResult{Outcome: adapter.OutcomeRateLimited, Reason: "rate_limited", Message: "The site is rate limiting requests."}
	case errors.Is(err, adapter.ErrParse):
		return &adapter.SigninResult{Outcome: adapter.OutcomeParseError, Reason: "parse", Message: "The response could not be parsed."}
	default:
		return &adapter.SigninResult{Outcome: adapter.OutcomeNetworkError, Reason: "network", Message: "The site could not be reached."}
	}
}

var (
	errNoLoginCredential = errors.New("no account name or login secret stored")
	errNoLoginForm       = errors.New("no login form found on the login page")
)

func errLoginRejected(msg string) error {
	if msg == "" {
		return errors.New("the site did not accept the credential")
	}
	return errors.New(msg)
}
