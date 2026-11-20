package pt

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

const maxSleep = 60 * time.Second

type rateConfig struct {
	Interval time.Duration
	Window   time.Duration
	Count    int
}

func rateConfigFor(t *PTTracker, def adapter.Defaults) rateConfig {
	interval := time.Duration(t.LimitIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Duration(def.LimitIntervalSeconds) * time.Second
	}
	count := t.LimitCount
	if count <= 0 {
		count = def.LimitCount
	}
	window := time.Duration(t.LimitSeconds) * time.Second
	if window <= 0 {
		window = time.Duration(def.LimitSeconds) * time.Second
	}
	return rateConfig{Interval: interval, Window: window, Count: count}
}

type deferredError struct {
	RetryAfter time.Duration
}

func (e *deferredError) Error() string {
	return fmt.Sprintf("pt: deferred for %s", e.RetryAfter.Round(time.Second))
}

type limiter struct {
	mu      sync.Mutex
	domains map[string]*domainLimit
}

type domainLimit struct {
	mu          sync.Mutex
	last        time.Time
	windowStart time.Time
	count       int
}

func newLimiter() *limiter { return &limiter{domains: map[string]*domainLimit{}} }

func (l *limiter) domain(name string) *domainLimit {
	l.mu.Lock()
	defer l.mu.Unlock()
	d, ok := l.domains[name]
	if !ok {
		d = &domainLimit{}
		l.domains[name] = d
	}
	return d
}

func (l *limiter) acquire(ctx context.Context, domain string, cfg rateConfig, now time.Time) error {
	d := l.domain(domain)
	d.mu.Lock()
	defer d.mu.Unlock()

	next := d.last.Add(cfg.Interval)
	if cfg.Count > 0 && cfg.Window > 0 {
		if d.windowStart.IsZero() || now.Sub(d.windowStart) >= cfg.Window {
			d.windowStart = now
			d.count = 0
		}
		if d.count >= cfg.Count {
			if windowEnd := d.windowStart.Add(cfg.Window); windowEnd.After(next) {
				next = windowEnd
			}
		}
	}

	if wait := next.Sub(now); wait > 0 {
		if wait > maxSleep {
			return &deferredError{RetryAfter: wait}
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	d.last = now
	if !d.last.After(next) {
		d.last = next
	}
	if cfg.Count > 0 && cfg.Window > 0 {
		d.count++
	}
	return nil
}
