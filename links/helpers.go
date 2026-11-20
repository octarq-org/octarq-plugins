package links

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

func normalizeTarget(raw string) (string, bool) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return raw, true
}

func validateRedirectTargets(l *Link) error {
	if l.ExpiredURL != "" {
		n, ok := normalizeTarget(strings.TrimSpace(l.ExpiredURL))
		if !ok {
			return huma.Error400BadRequest("Expired URL must be an http or https URL.")
		}
		l.ExpiredURL = n
	}
	for i := range l.RoutingRules {
		n, ok := normalizeTarget(strings.TrimSpace(l.RoutingRules[i].Target))
		if !ok {
			return huma.Error400BadRequest("Routing rule target must be an http or https URL.")
		}
		l.RoutingRules[i].Target = n
	}
	return nil
}

func quotaErrorToHTTP(err error, metric string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, plugin.ErrQuotaUnavailable) {
		return huma.Error402PaymentRequired(metric + " is not included in this plan. Upgrade your plan to unlock it.")
	}
	return huma.Error429TooManyRequests(metric + " quota exceeded for this workspace. Upgrade your plan or wait for the quota to reset.")
}

func (p *Plugin) checkQuota(ctx context.Context, orgID uint, metric string, n int64) error {
	if err := plugin.CheckQuota(p.ctx, ctx, orgID, metric, n); err != nil {
		return quotaErrorToHTTP(err, metric)
	}
	return nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return r.Replace(s)
}

func dialectDayBucket(db *gorm.DB) string {
	if db != nil && db.Name() == "postgres" {
		return "to_char(created_at, 'YYYY-MM-DD')"
	}
	return "strftime('%Y-%m-%d', created_at)"
}

func filterByTag(q *gorm.DB, tag string) *gorm.DB {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return q
	}
	folded := strings.ToLower(strings.ReplaceAll(tag, " ", ""))
	esc := escapeLike(folded)
	norm := "LOWER(REPLACE(tags, ' ', ''))"
	return q.Where(
		norm+" = ? OR "+norm+" LIKE ? ESCAPE '!' OR "+norm+" LIKE ? ESCAPE '!' OR "+norm+" LIKE ? ESCAPE '!'",
		folded,
		esc+",%",
		"%,"+esc,
		"%,"+esc+",%",
	)
}
