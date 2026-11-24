package links

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
)

type AbuseReport struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	OrgID       uint      `gorm:"column:owner_id;index" json:"-"`
	Slug        string    `gorm:"size:255;index" json:"slug"`
	Target      string    `gorm:"type:text" json:"target"`
	Reason      string    `gorm:"size:64" json:"reason"`
	Description string    `gorm:"type:text" json:"description"`
	ReporterIP  string    `gorm:"size:64" json:"reporterIp"`
	Status      string    `gorm:"size:32;default:'open'" json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (AbuseReport) TableName() string {
	return "abuse_reports"
}

var validAbuseReasons = map[string]bool{
	"spam":     true,
	"phishing": true,
	"malware":  true,
	"other":    true,
}

type SubmitAbuseInputBody struct {
	Slug        string `json:"slug"`
	Reason      string `json:"reason"`
	Description string `json:"description"`
}

type SubmitAbuseInput struct {
	Ctx  huma.Context `hidden:"true"`
	Body SubmitAbuseInputBody
}

func (i *SubmitAbuseInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type SubmitAbuseOutputBody struct {
	OK bool `json:"ok"`
	ID uint `json:"id"`
}

type SubmitAbuseOutput struct {
	Body SubmitAbuseOutputBody
}

func (p *Plugin) submitAbuse(ctx context.Context, input *SubmitAbuseInput) (*SubmitAbuseOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	ip := clientIP(r)
	if p.abuseLimiter != nil && !p.abuseLimiter.Allow(ip) {
		return nil, huma.Error429TooManyRequests("Too many abuse reports submitted from this address; wait a moment before trying again")
	}
	slug := strings.TrimSpace(input.Body.Slug)
	reason := strings.TrimSpace(strings.ToLower(input.Body.Reason))
	if slug == "" {
		return nil, huma.Error400BadRequest("Slug is required; specify a short link to report")
	}
	if !validAbuseReasons[reason] {
		return nil, huma.Error400BadRequest("Reason must be one of spam, phishing, malware, or other")
	}
	description := input.Body.Description
	if len(description) > 2000 {
		description = description[:2000]
	}
	var target string
	var orgID uint
	if t, o, ok := p.resolveSlug(r.Host, slug); ok {
		target = t
		orgID = o
	}
	rep := AbuseReport{
		OrgID:       orgID,
		Slug:        slug,
		Target:      target,
		Reason:      reason,
		Description: description,
		ReporterIP:  ip,
		Status:      "open",
	}
	if p.db != nil {
		if err := p.db.Create(&rep).Error; err != nil {
			return nil, huma.Error500InternalServerError("Could not submit abuse report; retry the request")
		}
	}
	p.notifyAbuse(rep)

	out := &SubmitAbuseOutput{}
	out.Body.OK = true
	out.Body.ID = rep.ID
	return out, nil
}

func (p *Plugin) notifyAbuse(rep AbuseReport) {
	if rep.OrgID == 0 {
		return
	}
	msg := fmt.Sprintf(
		"🚨 Abuse report #%d\nSlug: %s\nReason: %s\nTarget: %s\nDescription: %s",
		rep.ID, rep.Slug, rep.Reason, rep.Target, rep.Description,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctx = plugin.WithOrgID(ctx, rep.OrgID)

	payload := plugin.NotificationPayload{
		EventType: "abuse.report",
		Title:     fmt.Sprintf("🚨 Abuse Report #%d", rep.ID),
		Body:      msg,
		OrgID:     strconv.FormatUint(uint64(rep.OrgID), 10),
		Priority:  "high",
		Data: map[string]interface{}{
			"reportId":    rep.ID,
			"slug":        rep.Slug,
			"reason":      rep.Reason,
			"target":      rep.Target,
			"description": rep.Description,
		},
	}
	if p.emit != nil {
		if err := p.emit(ctx, payload); err != nil {
			slog.Warn("failed to emit abuse notification", "reportId", rep.ID, "err", err)
		}
	} else if p.publishEvent != nil {
		p.publishEvent(rep.OrgID, "abuse.report", payload.Data)
	}
}

func maskIP(ip string) string {
	if ip == "::1" || ip == "127.0.0.1" {
		return ip
	}
	if parts := strings.Split(ip, "."); len(parts) == 4 {
		return parts[0] + "." + parts[1] + "." + parts[2] + ".*"
	}
	if idx := strings.LastIndex(ip, ":"); idx >= 0 {
		return ip[:idx] + ":*"
	}
	return ip
}

func (p *Plugin) abuseDB(r *http.Request) (*gorm.DB, error) {
	if p.db == nil {
		return nil, huma.Error500InternalServerError("Database unavailable")
	}
	isInstAdmin := p.isInstanceAdmin != nil && p.isInstanceAdmin(r)
	orgID := p.orgID(r)
	isAdmin := p.hasRole(r, "admin") || isInstAdmin

	if !isAdmin {
		if orgID == 0 && (p.auth.UserID == nil || p.auth.UserID(r) == 0) && !isInstAdmin {
			return nil, huma.Error401Unauthorized("Authentication required; sign in to continue")
		}
		return nil, huma.Error403Forbidden("Admin role required; contact a workspace administrator")
	}

	if isInstAdmin {
		if orgID > 0 {
			return p.db.Where("owner_id = ? OR owner_id = 0", orgID), nil
		}
		return p.db.Where("owner_id = 0"), nil
	}

	if orgID > 0 {
		return p.db.Where("owner_id = ?", orgID), nil
	}

	return nil, huma.Error401Unauthorized("Authentication required; sign in to continue")
}

type ListAbuseReportsInput struct {
	Ctx    huma.Context `hidden:"true"`
	Status string       `query:"status"`
}

func (i *ListAbuseReportsInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type ListAbuseReportsOutput struct {
	Body []AbuseReport
}

func (p *Plugin) listAbuseReports(ctx context.Context, input *ListAbuseReportsInput) (*ListAbuseReportsOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	q, err := p.abuseDB(r)
	if err != nil {
		return nil, err
	}
	q = q.Order("created_at DESC")
	status := strings.TrimSpace(input.Status)
	if status == "" {
		q = q.Where("status = ?", "open")
	} else if status != "all" {
		q = q.Where("status = ?", status)
	}

	var reports []AbuseReport
	if err := q.Find(&reports).Error; err != nil {
		return nil, huma.Error500InternalServerError("Failed to query abuse reports")
	}
	for i := range reports {
		reports[i].ReporterIP = maskIP(reports[i].ReporterIP)
	}
	return &ListAbuseReportsOutput{Body: reports}, nil
}

type UpdateAbuseReportInputBody struct {
	Status string `json:"status"`
}

type UpdateAbuseReportInput struct {
	Ctx  huma.Context `hidden:"true"`
	ID   uint         `path:"id"`
	Body UpdateAbuseReportInputBody
}

func (i *UpdateAbuseReportInput) Resolve(ctx huma.Context) []error {
	i.Ctx = ctx
	return nil
}

type UpdateAbuseReportOutput struct {
	Body AbuseReport
}

func (p *Plugin) updateAbuseReport(ctx context.Context, input *UpdateAbuseReportInput) (*UpdateAbuseReportOutput, error) {
	if input.Ctx == nil {
		return nil, huma.Error500InternalServerError("Request context is unavailable. Try again.")
	}
	r, _ := humago.Unwrap(input.Ctx)
	q, err := p.abuseDB(r)
	if err != nil {
		return nil, err
	}
	var rep AbuseReport
	if err := q.First(&rep, input.ID).Error; err != nil {
		return nil, huma.Error404NotFound("Abuse report not found; verify the identifier and try again")
	}
	status := strings.TrimSpace(input.Body.Status)
	if status != "open" && status != "reviewed" && status != "dismissed" {
		return nil, huma.Error400BadRequest("Status must be open, reviewed, or dismissed")
	}
	if err := p.db.Model(&rep).Update("status", status).Error; err != nil {
		return nil, huma.Error500InternalServerError("Failed to update abuse report")
	}
	rep.Status = status
	rep.ReporterIP = maskIP(rep.ReporterIP)
	return &UpdateAbuseReportOutput{Body: rep}, nil
}

func (p *Plugin) SetAbuseRateLimit(limit int, window time.Duration) {
	p.abuseLimiter = newIPRateLimiter(limit, window)
}
