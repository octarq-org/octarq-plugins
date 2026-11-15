package adapter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"golang.org/x/net/html/charset"
)

// MaxBodyBytes caps every response. safehttp does not bound response size, and
// a tracker behind a broken proxy can otherwise stream for minutes.
const MaxBodyBytes = 8 << 20

// Page is a decoded response.
type Page struct {
	URL    *url.URL
	Status int
	Header http.Header
	Body   []byte
}

// Client is one tracker session: an injected *http.Client plus a cookie jar
// bound to the site origin.
//
// The *http.Client is injected rather than constructed here because production
// must use plugin/safehttp (the site URL comes from a user) while tests must
// reach a loopback fixture server, which safehttp deliberately blocks.
type Client struct {
	http    *http.Client
	jar     http.CookieJar
	base    *url.URL
	ua      string
	maxBody int64
}

// NewClient wraps base for one site. cookie is the stored Cookie header value
// and may be empty.
func NewClient(base *http.Client, site Site, cookie string) (*Client, error) {
	if base == nil {
		return nil, &OpError{Op: "client", Schema: site.Schema, Kind: KindConfig, Err: errors.New("http client is nil")}
	}
	origin, err := url.Parse(strings.TrimSpace(site.BaseURL))
	if err != nil || origin.Scheme == "" || origin.Host == "" {
		return nil, &OpError{Op: "client", Schema: site.Schema, Kind: KindConfig, Err: errors.New("site base URL is not absolute")}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, &OpError{Op: "client", Schema: site.Schema, Kind: KindConfig, Err: err}
	}

	// Copy the client struct so the jar and redirect policy are per-session
	// while the dialer and its SSRF guard stay shared.
	cloned := *base
	cloned.Jar = jar

	c := &Client{http: &cloned, jar: jar, base: origin, ua: site.UserAgent, maxBody: MaxBodyBytes}
	if !strings.EqualFold(site.BaseURL, origin.String()) {
		c.base = &url.URL{Scheme: origin.Scheme, Host: origin.Host}
	}
	if cookie != "" {
		c.SetCookieHeader(cookie)
	}
	return c, nil
}

// BaseOrigin returns the site origin without a trailing path.
func (c *Client) BaseOrigin() *url.URL { return c.base }

// SetCookieHeader loads a Cookie header value into the jar.
func (c *Client) SetCookieHeader(raw string) {
	cookies := ParseCookieHeader(raw)
	if len(cookies) == 0 {
		return
	}
	c.jar.SetCookies(c.base, cookies)
}

// Cookies serializes the jar for the site origin, which is the value stored in
// the credential vault.
func (c *Client) Cookies() string {
	return SerializeCookies(c.jar.Cookies(c.base))
}

// HasCookie reports whether the jar holds a named cookie.
func (c *Client) HasCookie(name string) bool {
	for _, ck := range c.jar.Cookies(c.base) {
		if ck.Name == name {
			return true
		}
	}
	return false
}

// NoRedirect returns a client that surfaces the first redirect response, which
// is how a successful form login is observed (302 + Set-Cookie).
func (c *Client) NoRedirect() *Client {
	cloned := *c
	inner := *c.http
	inner.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cloned.http = &inner
	return &cloned
}

// Get fetches rawURL, resolving it against the site origin when relative.
func (c *Client) Get(ctx context.Context, rawURL string, hdr map[string]string) (*Page, error) {
	req, err := c.newRequest(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return c.do(req)
}

// PostForm posts url-encoded form data to rawURL.
func (c *Client) PostForm(ctx context.Context, rawURL string, form url.Values, referer string) (*Page, error) {
	req, err := c.newRequest(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if referer != "" {
		if u, uerr := c.resolve(referer); uerr == nil {
			req.Header.Set("Referer", u.String())
		}
	}
	return c.do(req)
}

// PostJSON posts a JSON body with the given extra headers.
func (c *Client) PostJSON(ctx context.Context, rawURL string, body []byte, hdr map[string]string) (*Page, error) {
	req, err := c.newRequest(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return c.do(req)
}

func (c *Client) newRequest(ctx context.Context, method, rawURL string, body io.Reader) (*http.Request, error) {
	target, err := c.resolve(rawURL)
	if err != nil {
		return nil, &OpError{Op: "request", Kind: KindConfig, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, &OpError{Op: "request", Kind: KindConfig, Err: err}
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if c.ua != "" {
		req.Header.Set("User-Agent", c.ua)
	}
	return req, nil
}

func (c *Client) resolve(rawURL string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	if !u.IsAbs() {
		u = c.base.ResolveReference(u)
	}
	return u, nil
}

func (c *Client) do(req *http.Request) (*Page, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, &OpError{Op: req.Method, Kind: KindNetwork, Err: err}
		}
		return nil, &OpError{Op: req.Method, Kind: KindNetwork, Err: err}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBody+1))
	if err != nil {
		return nil, &OpError{Op: req.Method, Kind: KindNetwork, Status: resp.StatusCode, Err: err}
	}
	if int64(len(raw)) > c.maxBody {
		return nil, &OpError{Op: req.Method, Kind: KindUpstream, Status: resp.StatusCode, Err: errors.New("response body exceeds limit")}
	}

	decoded := DecodeCharset(raw, resp.Header.Get("Content-Type"))
	if IsChallenge(resp.Header, decoded) {
		return nil, &OpError{Op: req.Method, Kind: KindCloudflare, Status: resp.StatusCode, ManualReason: "cloudflare"}
	}

	return &Page{URL: resp.Request.URL, Status: resp.StatusCode, Header: resp.Header, Body: decoded}, nil
}

// DecodeCharset converts a page body to UTF-8 using the response Content-Type
// or the HTML meta tag. Many Chinese trackers are still GBK.
func DecodeCharset(body []byte, contentType string) []byte {
	if len(body) == 0 {
		return body
	}
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return body
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		return body
	}
	return out
}

// challengeMarkers are the fingerprints of an interstitial anti-bot page.
var challengeMarkers = [][]byte{
	[]byte("cf-please-wait"),
	[]byte("Just a moment..."),
	[]byte("challenge-platform"),
	[]byte("cf_chl_opt"),
	[]byte("DDOS-GUARD"),
	[]byte("请稍候…"),
}

// IsChallenge reports whether a response is an anti-bot interstitial rather
// than site content.
func IsChallenge(hdr http.Header, body []byte) bool {
	if hdr != nil {
		if v := strings.ToLower(strings.TrimSpace(hdr.Get("cf-mitigated"))); v == "challenge" {
			return true
		}
	}
	head := body
	if len(head) > 64<<10 {
		head = head[:64<<10]
	}
	for _, marker := range challengeMarkers {
		if bytes.Contains(head, marker) {
			return true
		}
	}
	return false
}

// ParseCookieHeader parses a browser Cookie header value. Values may contain
// '=' (base64), so only the first '=' separates name from value.
func ParseCookieHeader(raw string) []*http.Cookie {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []*http.Cookie
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, found := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			continue
		}
		out = append(out, &http.Cookie{Name: name, Value: strings.TrimSpace(value)})
	}
	return out
}

// SerializeCookies renders cookies as a Cookie header value.
func SerializeCookies(cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, ck := range cookies {
		if ck == nil || ck.Name == "" {
			continue
		}
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	return strings.Join(parts, "; ")
}
