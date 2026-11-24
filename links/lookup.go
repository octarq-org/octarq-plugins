package links

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"
)

const clickLimitCacheTTL = 5 * time.Second

func (e *Engine) Lookup(host, slug string) (*Link, bool) {
	host = stripPort(host)
	ctx := context.Background()

	var link Link
	cacheKey := "link:redirect:" + host + ":" + slug

	if e.ctx != nil && e.ctx.CacheGet != nil && e.ctx.CacheGet(ctx, cacheKey, &link) {
		if link.ID == 0 {
			return nil, false
		}
		return &link, true
	}

	query, servable := e.scopeForHost(slug, host)
	if !servable {
		e.cacheNegative(ctx, cacheKey)
		return nil, false
	}

	err := query.
		Order("host DESC").
		First(&link).Error
	if err != nil {
		e.cacheNegative(ctx, cacheKey)
		return nil, false
	}
	if !link.Enabled || link.Archived {
		e.cacheNegative(ctx, cacheKey)
		return nil, false
	}
	if link.Host != "" && e.linkHostDisabled(link.OrgID, host) {
		return nil, false
	}

	if e.ctx != nil && e.ctx.CacheSet != nil {
		ttl := time.Hour
		if link.ClickLimit > 0 {
			ttl = clickLimitCacheTTL
		}
		if err := e.ctx.CacheSet(ctx, cacheKey, &link, ttl); err != nil {
			log.Printf("links: cacheSet failed for key %s: %v", cacheKey, err)
		}
	}
	return &link, true
}

func (e *Engine) scopeForHost(slug, host string) (*gorm.DB, bool) {
	if e.hosts != nil {
		if owner, ok := e.hosts.OwnerOf(host); ok {
			return e.db.Where("slug = ? AND (host = ? OR host = '')", slug, host).
				Where("owner_id = ?", owner), true
		}
		if e.hosts.ServesTraffic(host) {
			return nil, false
		}
	}
	return e.db.Where("slug = ? AND host = ''", slug), true
}

func (e *Engine) cacheNegative(ctx context.Context, cacheKey string) {
	var empty Link
	if e.ctx != nil && e.ctx.CacheSet != nil {
		if err := e.ctx.CacheSet(ctx, cacheKey, &empty, time.Minute); err != nil {
			log.Printf("links: cacheNegative failed for key %s: %v", cacheKey, err)
		}
	}
}

func (e *Engine) linkHostDisabled(orgID uint, host string) bool {
	if orgID == 0 {
		return false
	}
	normHost := normalizeHost(host)
	var doms []domainLinkRow
	e.db.Table("domains").Where("owner_id = ? AND for_link = ?", orgID, true).Find(&doms)
	listed := false
	for _, d := range doms {
		for _, h := range d.LinkHosts {
			if normalizeHost(h.Host) == normHost {
				listed = true
				if h.Enabled {
					return false
				}
			}
		}
	}
	return listed
}

func expired(link *Link) bool {
	if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
		return true
	}
	if link.ClickLimit > 0 && link.Clicks >= link.ClickLimit {
		return true
	}
	return false
}
