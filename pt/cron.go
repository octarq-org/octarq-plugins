package pt

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	cronTickJob   = "pt.tick"
	cronTickSpec  = "*/5 * * * *"
	cronTickEvery = 5 * time.Minute

	cronRetentionJob  = "pt.retention"
	cronRetentionSpec = "0 4 * * *"

	snapshotRetention = 90 * 24 * time.Hour
	signinRetention   = 180 * 24 * time.Hour

	maxConcurrent = 4
	tickBudget    = 8 * time.Minute
)

func (p *Plugin) limiterRef() *limiter {
	p.limiterOnce.Do(func() { p.limiterVal = newLimiter() })
	return p.limiterVal
}

func (p *Plugin) runTick(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, tickBudget)
	defer cancel()

	orgIDs, err := p.orgIDs(ctx)
	if err != nil {
		return err
	}
	now := p.clock()

	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrent)
	for _, orgID := range orgIDs {
		due, listErr := p.dueTrackers(gctx, orgID, now)
		if listErr != nil {
			continue
		}
		for i := range due {
			tracker := due[i]
			group.Go(func() error {
				if gctx.Err() != nil {
					return nil
				}
				p.runTracker(gctx, &tracker, now)
				return nil
			})
		}
	}
	return group.Wait()
}

func (p *Plugin) runTracker(ctx context.Context, t *PTTracker, now time.Time) {
	if t.NeedsManualAction {
		return
	}

	if t.SigninEnabled && !t.NeedsManualAction && dueAt(t.NextSigninAt, now) {
		if !inSigninWindow(t, now) {
			next := planSigninAt(t, now)
			p.patchTracker(ctx, t, map[string]any{"next_signin_at": next})
		} else if _, err := p.signin(ctx, t, triggerCron, false); err != nil {
			p.handleTickError(ctx, t, err, now, "next_signin_at")
		}
	}

	if t.StatsIntervalHours > 0 && dueAt(t.NextStatsAt, now) {
		if _, err := p.refresh(ctx, t, triggerCron); err != nil {
			p.handleTickError(ctx, t, err, now, "next_stats_at")
		}
	}
}

func dueAt(at *time.Time, now time.Time) bool {
	return at == nil || !at.After(now)
}

func (p *Plugin) handleTickError(ctx context.Context, t *PTTracker, err error, now time.Time, column string) {
	var deferred *deferredError
	if errors.As(err, &deferred) {
		p.patchTracker(ctx, t, map[string]any{column: now.Add(cronTickEvery)})
		return
	}

	delay := retryDelay(t.ConsecutiveFailures+1, outcomeFromError(err))
	if delay <= 0 {
		p.patchTracker(ctx, t, map[string]any{column: nil})
		return
	}
	next := now.Add(delay)
	if column == "next_stats_at" && t.StatsIntervalHours > 0 {
		if interval := time.Duration(t.StatsIntervalHours) * time.Hour; next.After(now.Add(interval)) {
			next = now.Add(interval)
		}
	}
	p.patchTracker(ctx, t, map[string]any{column: next})
}

func (p *Plugin) runRetention(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, tickBudget)
	defer cancel()

	orgIDs, err := p.orgIDs(ctx)
	if err != nil {
		return err
	}
	now := p.clock()
	for _, orgID := range orgIDs {
		if err := p.pruneOldRows(ctx, orgID, now.Add(-snapshotRetention), now.Add(-signinRetention)); err != nil {
			continue
		}
	}
	return nil
}
