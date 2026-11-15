package nexusphp

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// parseDoc builds a goquery document from a response body.
func parseDoc(body []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(bytes.NewReader(body))
}

// visibleText returns the page text with scripts, styles, templates and form
// controls removed, so evidence matching cannot fire on a hidden element or on
// an inline script string.
func visibleText(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	clone := goquery.CloneDocument(doc)
	clone.Find("script, style, template, noscript, textarea, input[type=hidden]").Remove()
	return strings.Join(strings.Fields(clone.Text()), " ")
}

// hasLoginForm reports whether the page still shows a login form, which is the
// most reliable "not authenticated" signal across NexusPHP skins.
func hasLoginForm(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	found := false
	doc.Find("input").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		name, _ := sel.Attr("name")
		typ, _ := sel.Attr("type")
		if strings.EqualFold(typ, loginFieldPass) || strings.EqualFold(name, loginFieldPass) || strings.EqualFold(name, loginFieldName) {
			found = true
			return false
		}
		return true
	})
	return found
}

// hasLogoutLink reports a logout affordance, the positive "authenticated"
// signal in NexusPHP.
func hasLogoutLink(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	markers := []string{"logout", "退出", "登出", "usercp", "mybonus"}
	found := false
	doc.Find("a").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		href, _ := sel.Attr("href")
		text := sel.Text()
		lower := strings.ToLower(href)
		for _, m := range markers {
			if strings.Contains(lower, m) || strings.Contains(text, m) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// loggedIn combines the two signals: no login form and a logout/usercp link.
func loggedIn(doc *goquery.Document) bool {
	if doc == nil || hasLoginForm(doc) {
		return false
	}
	return hasLogoutLink(doc)
}

var userIDRe = regexp.MustCompile(`userdetails\.php\?id=(\d+)`)

// userIDFromHTML extracts the site user id from any link on the page.
func userIDFromHTML(body []byte) string {
	if m := userIDRe.FindSubmatch(body); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

// usernameFromDoc reads the display name next to the userdetails link.
func usernameFromDoc(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	name := ""
	doc.Find("a[href*=userdetails]").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		text := strings.TrimSpace(sel.Find("b").First().Text())
		if text == "" {
			text = strings.TrimSpace(sel.Text())
		}
		if text != "" {
			name = text
			return false
		}
		return true
	})
	return name
}

// detectCaptcha reports whether the page demands a captcha. A captcha is a hard
// stop: guessing one gets the account flagged, and OCR is out of scope.
func detectCaptcha(doc *goquery.Document, body []byte) bool {
	if doc != nil {
		found := false
		doc.Find("input").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			name, _ := sel.Attr("name")
			id, _ := sel.Attr("id")
			for _, marker := range []string{"imagestring", "imagehash", "captcha", "verify_code", "code"} {
				if strings.Contains(strings.ToLower(name), marker) || strings.Contains(strings.ToLower(id), marker) {
					found = true
					return false
				}
			}
			return true
		})
		if found {
			return true
		}
		doc.Find("img").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			src, _ := sel.Attr("src")
			alt, _ := sel.Attr("alt")
			for _, marker := range []string{"image.php", "captcha", "verify"} {
				if strings.Contains(strings.ToLower(src), marker) || strings.Contains(strings.ToLower(alt), marker) {
					found = true
					return false
				}
			}
			return true
		})
		return found
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "imagestring") || strings.Contains(lower, "验证码") || strings.Contains(lower, "驗證碼")
}

// containsAny reports whether text contains any marker.
func containsAny(text string, markers ...string) bool {
	for _, m := range markers {
		if m != "" && strings.Contains(text, m) {
			return true
		}
	}
	return false
}
