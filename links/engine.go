package links

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/octarq-org/octarq/server/pkg/safego"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

type clickItem struct {
	orgID       uint
	slug        string
	linkID      uint
	ip          string
	country     string
	region      string
	city        string
	ua          string
	device      string
	browser     string
	osStr       string
	bot         bool
	referer     string
	fingerprint string
	variant     string
	utmSource   string
	utmMedium   string
	utmCampaign string
	createdAt   time.Time
}

type HostResolver interface {
	OwnerOf(host string) (uint, bool)
	ServesTraffic(host string) bool
}

type Engine struct {
	db          *gorm.DB
	hosts       HostResolver
	ctx         *plugin.Context
	host        plugin.Host
	queue       chan clickItem
	wg          sync.WaitGroup
	closeOnce   sync.Once
	dropCount   atomic.Uint64
	txCount     atomic.Uint64
	rateLimiter *ipRateLimiter
}

func NewEngine(db *gorm.DB, ctx *plugin.Context) *Engine {
	h := plugin.EnsureHost(ctx)
	var resolver HostResolver
	if h != nil && h.Hosts() != nil {
		resolver = h.Hosts()
	}
	e := &Engine{
		db: db, hosts: resolver, ctx: ctx, host: h,
		queue: make(chan clickItem, 5000), rateLimiter: newIPRateLimiter(300, time.Minute),
	}
	e.wg.Add(1)
	go e.worker()
	return e
}

func (e *Engine) SetHosts(resolver HostResolver) {
	e.hosts = resolver
}

func (e *Engine) SetRateLimit(limit int, window time.Duration) {
	e.rateLimiter = newIPRateLimiter(limit, window)
}

func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		close(e.queue)
		e.wg.Wait()
	})
}

func (e *Engine) DropCount() uint64 {
	return e.dropCount.Load()
}

func (e *Engine) TxCount() uint64 {
	return e.txCount.Load()
}

func (e *Engine) worker() {
	defer e.wg.Done()
	batch := make([]clickItem, 0, 100)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		e.flushBatch(batch)
		batch = batch[:0]
	}

	consume := func() (closed bool) {
		defer safego.Recover("links.click-worker")
		for {
			select {
			case item, ok := <-e.queue:
				if !ok {
					flush()
					return true
				}
				batch = append(batch, item)
				if len(batch) >= 100 {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}

	for !consume() {
		batch = batch[:0]
	}
}

func (e *Engine) flushBatch(batch []clickItem) {
	if len(batch) == 0 {
		return
	}

	clicksByOrg := make(map[uint]int64)
	for _, item := range batch {
		if !item.bot {
			clicksByOrg[item.orgID]++
		}
	}

	suppressed := make(map[uint]bool)
	for orgID, count := range clicksByOrg {
		if errors.Is(plugin.CheckQuota(e.ctx, context.Background(), orgID, "clicksPerMonth", count), plugin.ErrQuotaExceeded) {
			suppressed[orgID] = true
		}
	}

	events := make([]LinkEvent, 0, len(batch))
	clicksByLink := make(map[uint]int64)
	for _, item := range batch {
		if suppressed[item.orgID] {
			continue
		}
		events = append(events, LinkEvent{
			LinkID:      item.linkID,
			CreatedAt:   item.createdAt,
			IP:          item.ip,
			Country:     item.country,
			Region:      item.region,
			City:        item.city,
			Device:      item.device,
			Browser:     item.browser,
			OS:          item.osStr,
			Referer:     item.referer,
			UA:          item.ua,
			Fingerprint: item.fingerprint,
			IsBot:       item.bot,
			Variant:     item.variant,
			UTMSource:   item.utmSource,
			UTMMedium:   item.utmMedium,
			UTMCampaign: item.utmCampaign,
		})
		if !item.bot {
			clicksByLink[item.linkID]++
		}
	}

	err := e.db.Transaction(func(tx *gorm.DB) error {
		if len(events) > 0 {
			if err := tx.Create(&events).Error; err != nil {
				return err
			}
		}
		if len(clicksByLink) > 0 {
			var queryBuilder strings.Builder
			queryBuilder.WriteString("UPDATE links SET clicks = clicks + CASE id")

			args := make([]interface{}, 0, len(clicksByLink)*2+1)
			ids := make([]uint, 0, len(clicksByLink))

			for linkID, count := range clicksByLink {
				queryBuilder.WriteString(" WHEN ? THEN ?")
				args = append(args, linkID, count)
				ids = append(ids, linkID)
			}

			queryBuilder.WriteString(" ELSE 0 END WHERE id IN ?")
			args = append(args, ids)

			if err := tx.Exec(queryBuilder.String(), args...).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err == nil {
		e.txCount.Add(1)
	} else {
		slog.Error("failed to flush click events batch", "count", len(batch), "err", err)
	}

	if e.ctx != nil {
		if e.ctx.RecordUsage != nil {
			for orgID, count := range clicksByOrg {
				if suppressed[orgID] {
					continue
				}
				e.ctx.RecordUsage(orgID, plugin.MetricClicks, count)
			}
		}
		if e.host != nil && e.host.Events() != nil {
			for _, item := range batch {
				e.host.Events().PublishEvent(item.orgID, "link.click", map[string]any{
					"linkId":    item.linkID,
					"slug":      item.slug,
					"ip":        item.ip,
					"country":   item.country,
					"region":    item.region,
					"city":      item.city,
					"device":    item.device,
					"browser":   item.browser,
					"os":        item.osStr,
					"referer":   item.referer,
					"isBot":     item.bot,
					"timestamp": item.createdAt,
				})
			}
		}
	}
}
