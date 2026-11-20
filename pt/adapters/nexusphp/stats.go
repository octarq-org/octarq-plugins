package nexusphp

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

var (
	uploadRe    = regexp.MustCompile(`上[传傳]量[^\d]{0,40}([\d.,]+\s*[KMGTPE]?i?B)`)
	downloadRe  = regexp.MustCompile(`下[载載]量[^\d]{0,40}([\d.,]+\s*[KMGTPE]?i?B)`)
	ratioRe     = regexp.MustCompile(`分享率[^\d∞]{0,40}(∞|[\d.,]+)`)
	bonusTextRe = regexp.MustCompile(`(?:魔力值|魔法值|积分|積分)[^\d]{0,40}([\d.,]+)`)
	numberRe    = regexp.MustCompile(`[\d]+(?:[.,]\d+)*`)
)

var errStatsIncomplete = errors.New("upload and download values were not found on the index page")

func stats(ctx context.Context, s *adapter.Session) (*adapter.UserStats, error) {
	p, err := page(ctx, s, "/index.php")
	if err != nil {
		return nil, err
	}
	if !isOK(p) {
		return nil, &adapter.OpError{Op: "stats", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindUpstream, Status: p.Status}
	}

	doc, err := parseDoc(p.Body)
	if err != nil {
		return nil, &adapter.OpError{Op: "stats", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindParse, Err: err}
	}
	if hasLoginForm(doc) {
		return nil, &adapter.OpError{Op: "stats", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindCookieExpired}
	}

	out := &adapter.UserStats{
		Username: usernameFromDoc(doc),
		UserID:   userIDFromHTML(p.Body),
	}
	text := visibleText(doc)

	out.UploadedBytes = sizeFromMatch(uploadRe, text)
	out.DownloadedBytes = sizeFromMatch(downloadRe, text)
	out.Ratio, out.RatioInfinite = ratioFromText(text)
	out.Bonus = bonusFromDoc(doc, text)

	if out.UserID != "" {
		if detail, derr := page(ctx, s, "/userdetails.php?id="+out.UserID); derr == nil {
			parseUserDetail(detail, out, siteTimezone(s))
		}
	}

	if out.UploadedBytes == nil || out.DownloadedBytes == nil {
		return nil, &adapter.OpError{Op: "stats", Schema: adapter.SchemaNexusPHP, Kind: adapter.KindParse, Err: errStatsIncomplete}
	}
	return out, nil
}

func sizeFromMatch(re *regexp.Regexp, text string) *int64 {
	m := re.FindStringSubmatch(text)
	if len(m) != 2 {
		return nil
	}
	value, ok := adapter.ParseSize(m[1])
	if !ok {
		return nil
	}
	return value
}

func ratioFromText(text string) (*float64, bool) {
	m := ratioRe.FindStringSubmatch(text)
	if len(m) != 2 {
		return nil, false
	}
	value, infinite, ok := adapter.ParseRatio(m[1])
	if !ok {
		return nil, false
	}
	return value, infinite
}

func bonusFromDoc(doc *goquery.Document, text string) *float64 {
	if doc != nil {
		value := ""
		doc.Find("a[href*=mybonus]").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
			if m := numberRe.FindString(sel.Text()); m != "" {
				value = m
				return false
			}
			return true
		})
		if value != "" {
			if parsed, ok := parseBonus(value); ok {
				return parsed
			}
		}
	}
	if m := bonusTextRe.FindStringSubmatch(text); len(m) == 2 {
		if parsed, ok := parseBonus(m[1]); ok {
			return parsed
		}
	}
	return nil
}

func parseBonus(raw string) (*float64, bool) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), ",", "")
	v, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return nil, false
	}
	return &v, true
}

func parseUserDetail(p *adapter.Page, out *adapter.UserStats, loc *time.Location) {
	doc, err := parseDoc(p.Body)
	if err != nil {
		return
	}
	doc.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		cells := tr.Find("td")
		if cells.Length() < 2 {
			return
		}
		label := strings.TrimSpace(cells.Eq(0).Text())
		value := strings.TrimSpace(cells.Eq(1).Text())
		if label == "" {
			return
		}
		switch {
		case containsAny(label, "等级", "等級", "用户等级", "用戶等級"):
			level := strings.TrimSpace(cells.Eq(1).Find("img").First().AttrOr("title", ""))
			if level == "" {
				level = value
			}
			if level != "" {
				out.UserLevel = level
			}
		case containsAny(label, "加入日期", "注册日期", "註冊日期"):
			if parsed, ok := adapter.ParseDate(value, loc); ok {
				out.JoinAt = parsed
			}
		case containsAny(label, "警告"):
			if n, ok := adapter.ParseCount(value); ok {
				out.WarningCount = n
			}
		case containsAny(label, "邀请", "邀請"):
			if n, ok := adapter.ParseCount(value); ok {
				out.Invites = n
			}
		case containsAny(label, "做种", "做種", "当前上传", "當前上傳"):
			if n, ok := adapter.ParseCount(value); ok {
				out.Seeding = n
			}
		case containsAny(label, "下载中", "下載中", "当前下载", "當前下載"):
			if n, ok := adapter.ParseCount(value); ok {
				out.Leeching = n
			}
		}
	})
}
