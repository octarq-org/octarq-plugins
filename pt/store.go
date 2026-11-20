package pt

import (
	"context"
	"errors"
	"time"

	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("pt: not found")

var errOwnerRequired = errors.New("pt: owner user id is required")

var errTenantUnavailable = errors.New("pt: tenant database unavailable")

func (p *Plugin) tenantDB(ctx context.Context, orgID uint) (*plugin.TenantDB, error) {
	if orgID == 0 {
		return nil, errTenantUnavailable
	}
	if host := p.hostRef(); host != nil {
		if tdb := host.TenantDB(orgID); tdb != nil {
			return tdb.WithColumn("org_id").WithContext(ctx), nil
		}
	}
	if db := p.dbRef(); db != nil {
		tdb, err := plugin.NewTenantDB(db, orgID)
		if err == nil && tdb != nil {
			return tdb.WithColumn("org_id").WithContext(ctx), nil
		}
	}
	return nil, errTenantUnavailable
}

func (p *Plugin) listTrackers(ctx context.Context, orgID, ownerUserID uint) ([]PTTracker, error) {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var out []PTTracker
	if err := tdb.Scoped(&PTTracker{}).Where("owner_user_id = ?", ownerUserID).Order("name ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Plugin) findTracker(ctx context.Context, orgID, ownerUserID, id uint) (*PTTracker, error) {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var t PTTracker
	if err := tdb.Scoped(&PTTracker{}).Where("owner_user_id = ? AND id = ?", ownerUserID, id).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (p *Plugin) findTrackerByName(ctx context.Context, orgID, ownerUserID uint, name string) (*PTTracker, error) {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var t PTTracker
	if err := tdb.Scoped(&PTTracker{}).Where("owner_user_id = ? AND name = ?", ownerUserID, name).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (p *Plugin) createTracker(ctx context.Context, t *PTTracker) error {
	if t.OwnerUserID == 0 {
		return errOwnerRequired
	}
	tdb, err := p.tenantDB(ctx, t.OrgID)
	if err != nil {
		return err
	}
	return tdb.Create(t).Error
}

func (p *Plugin) saveTracker(ctx context.Context, t *PTTracker) error {
	tdb, err := p.tenantDB(ctx, t.OrgID)
	if err != nil {
		return err
	}
	return tdb.Scoped(&PTTracker{}).Where("id = ?", t.ID).Updates(trackerUpdates(t)).Error
}

func trackerUpdates(t *PTTracker) map[string]any {
	return map[string]any{
		"name":                    t.Name,
		"base_url":                t.BaseURL,
		"api_base_url":            t.APIBaseURL,
		"domain":                  t.Domain,
		"schema_key":              t.SchemaKey,
		"username":                t.Username,
		"user_agent":              t.UserAgent,
		"timezone":                t.Timezone,
		"enabled":                 t.Enabled,
		"signin_enabled":          t.SigninEnabled,
		"signin_window_start_min": t.SigninWindowStartMin,
		"signin_window_end_min":   t.SigninWindowEndMin,
		"stats_interval_hours":    t.StatsIntervalHours,
		"timeout_seconds":         t.TimeoutSeconds,
		"limit_interval_seconds":  t.LimitIntervalSeconds,
		"limit_count":             t.LimitCount,
		"limit_seconds":           t.LimitSeconds,
		"updated_at":              t.UpdatedAt,
	}
}

func (p *Plugin) deleteTrackerCascade(ctx context.Context, orgID, trackerID uint) error {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return err
	}
	return tdb.Transaction(func(tx *plugin.TenantDB) error {
		if err := tx.Scoped(&PTSnapshot{}).Where("tracker_id = ?", trackerID).Delete(&PTSnapshot{}).Error; err != nil {
			return err
		}
		if err := tx.Scoped(&PTSigninResult{}).Where("tracker_id = ?", trackerID).Delete(&PTSigninResult{}).Error; err != nil {
			return err
		}
		return tx.Scoped(&PTTracker{}).Where("id = ?", trackerID).Delete(&PTTracker{}).Error
	})
}

func (p *Plugin) latestSnapshot(ctx context.Context, orgID, trackerID uint) (*PTSnapshot, error) {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var s PTSnapshot
	if err := tdb.Scoped(&PTSnapshot{}).Where("tracker_id = ?", trackerID).Order("captured_at DESC, id DESC").First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (p *Plugin) listSnapshots(ctx context.Context, orgID, trackerID uint, limit int) ([]PTSnapshot, error) {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 90
	}
	var out []PTSnapshot
	if err := tdb.Scoped(&PTSnapshot{}).Where("tracker_id = ?", trackerID).Order("captured_at DESC, id DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Plugin) addSnapshot(ctx context.Context, s *PTSnapshot) error {
	tdb, err := p.tenantDB(ctx, s.OrgID)
	if err != nil {
		return err
	}
	return tdb.Create(s).Error
}

func (p *Plugin) addSignin(ctx context.Context, r *PTSigninResult) error {
	tdb, err := p.tenantDB(ctx, r.OrgID)
	if err != nil {
		return err
	}
	return tdb.Create(r).Error
}

func (p *Plugin) listSignins(ctx context.Context, orgID, trackerID uint, limit int) ([]PTSigninResult, error) {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 365 {
		limit = 30
	}
	var out []PTSigninResult
	if err := tdb.Scoped(&PTSigninResult{}).Where("tracker_id = ?", trackerID).Order("attempted_at DESC, id DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Plugin) latestSignin(ctx context.Context, orgID, trackerID uint) (*PTSigninResult, error) {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var r PTSigninResult
	if err := tdb.Scoped(&PTSigninResult{}).Where("tracker_id = ?", trackerID).Order("attempted_at DESC, id DESC").First(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

func (p *Plugin) claimRequestSlot(ctx context.Context, orgID, trackerID uint, now, earliest time.Time) (bool, error) {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return false, err
	}
	res := tdb.Scoped(&PTTracker{}).
		Where("id = ? AND (last_request_at IS NULL OR last_request_at <= ?)", trackerID, earliest).
		Update("last_request_at", now)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (p *Plugin) dueTrackers(ctx context.Context, orgID uint, now time.Time) ([]PTTracker, error) {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var out []PTTracker
	err = tdb.Scoped(&PTTracker{}).
		Where("enabled = ?", true).
		Where("needs_manual_action = ?", false).
		Where("(signin_enabled = ? AND (next_signin_at IS NULL OR next_signin_at <= ?)) OR (stats_interval_hours > 0 AND (next_stats_at IS NULL OR next_stats_at <= ?))", true, now, now).
		Order("id ASC").
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Plugin) orgIDs(ctx context.Context) ([]uint, error) {
	db := p.dbRef()
	if db == nil {
		return nil, errTenantUnavailable
	}
	var out []uint
	if err := db.WithContext(ctx).Model(&PTTracker{}).Distinct().Pluck("org_id", &out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Plugin) pruneOldRows(ctx context.Context, orgID uint, snapshotBefore, signinBefore time.Time) error {
	tdb, err := p.tenantDB(ctx, orgID)
	if err != nil {
		return err
	}
	if err := tdb.Scoped(&PTSnapshot{}).Where("captured_at < ?", snapshotBefore).Delete(&PTSnapshot{}).Error; err != nil {
		return err
	}
	return tdb.Scoped(&PTSigninResult{}).Where("attempted_at < ?", signinBefore).Delete(&PTSigninResult{}).Error
}
