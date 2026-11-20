package links

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"gorm.io/gorm"
)

func TestLinks_CrossTenantAttack_AntiPenetration(t *testing.T) {
	t.Parallel()

	p, mkCtx := setupFullLinksTestDB(t)
	ctx := context.Background()

	link1 := Link{
		OrgID:   1,
		Slug:    "org1-slug",
		Target:  "https://org1.example.com",
		Title:   "Org 1 Resource",
		Enabled: true,
	}
	link2 := Link{
		OrgID:   2,
		Slug:    "org2-slug",
		Target:  "https://org2.example.com",
		Title:   "Org 2 Resource",
		Enabled: true,
	}
	if err := p.db.Create(&link1).Error; err != nil {
		t.Fatalf("failed to seed link1: %v", err)
	}
	if err := p.db.Create(&link2).Error; err != nil {
		t.Fatalf("failed to seed link2: %v", err)
	}

	t.Run("UpdateOtherTenantLink_Returns404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/links/update", nil)
		req.Header.Set("X-Org-ID", "1")
		humaCtx := mkCtx(req)

		updateIn := &UpdateLinkInput{
			Ctx: humaCtx,
			ID:  link2.ID,
			Body: linkDTO{
				Slug:   "hacked-org2-slug",
				Target: "https://attacker.example.com",
				Title:  "Hacked Link",
			},
		}
		_ = updateIn.Resolve(humaCtx)

		_, err := p.updateLink(ctx, updateIn)
		if err == nil {
			t.Fatalf("expected error when tenant 1 updates tenant 2's link, got nil")
		}
		hErr, ok := err.(huma.StatusError)
		if !ok || hErr.GetStatus() != http.StatusNotFound {
			t.Fatalf("expected 404 NotFound, got: %v", err)
		}

		var check Link
		if err := p.db.First(&check, link2.ID).Error; err != nil {
			t.Fatalf("failed to reload link2: %v", err)
		}
		if check.Slug != "org2-slug" || check.Target != "https://org2.example.com" {
			t.Fatalf("link2 was modified despite 404: slug=%s, target=%s", check.Slug, check.Target)
		}
	})

	t.Run("DeleteOtherTenantLink_Returns404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/links/delete", nil)
		req.Header.Set("X-Org-ID", "1")
		humaCtx := mkCtx(req)

		delIn := &DeleteLinkInput{
			Ctx: humaCtx,
			ID:  link2.ID,
		}
		_ = delIn.Resolve(humaCtx)

		_, err := p.deleteLink(ctx, delIn)
		if err == nil {
			t.Fatalf("expected error when tenant 1 deletes tenant 2's link, got nil")
		}
		hErr, ok := err.(huma.StatusError)
		if !ok || hErr.GetStatus() != http.StatusNotFound {
			t.Fatalf("expected 404 NotFound, got: %v", err)
		}

		var check Link
		if err := p.db.First(&check, link2.ID).Error; err != nil {
			t.Fatalf("link2 was deleted by tenant 1: %v", err)
		}
	})

	t.Run("ExportCSV_DoesNotExposeOtherTenant", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/links/export", nil)
		req.Header.Set("X-Org-ID", "1")
		humaCtx := humago.NewContext(nil, req, rec)

		exportIn := &ExportLinksCSVInput{Ctx: humaCtx}
		_ = exportIn.Resolve(humaCtx)

		_, err := p.exportLinksCSV(ctx, exportIn)
		if err != nil {
			t.Fatalf("unexpected export error: %v", err)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "org1-slug") {
			t.Errorf("expected CSV to contain org1-slug, got: %s", body)
		}
		if strings.Contains(body, "org2-slug") {
			t.Errorf("CSV leaked tenant 2 data: %s", body)
		}
	})

	t.Run("DirectTenantDB_FailClosedEnforcement", func(t *testing.T) {
		tdb1 := p.tenantDB(1)
		if tdb1 == nil {
			t.Fatalf("tenantDB(1) is nil")
		}

		spoofedLink := Link{
			OrgID:   2,
			Slug:    "spoofed-slug",
			Target:  "https://spoofed.example.com",
			Enabled: true,
		}
		if err := tdb1.Create(&spoofedLink).Error; err == nil {
			t.Fatalf("expected error when inserting OrgID 2 into TenantDB(1), got nil")
		}

		spoofedSave := Link{
			ID:      link2.ID,
			OrgID:   2,
			Slug:    "org2-slug-hacked",
			Target:  "https://spoofed.example.com",
			Enabled: true,
		}
		if err := tdb1.Save(&spoofedSave).Error; err == nil {
			t.Fatalf("expected error when saving OrgID 2 into TenantDB(1), got nil")
		}

		var forbidden Link
		if err := tdb1.Where("id = ?", link2.ID).First(&forbidden).Error; err == nil {
			t.Fatalf("tenant 1 TenantDB read tenant 2 link: %+v", forbidden)
		} else if err != gorm.ErrRecordNotFound {
			t.Fatalf("expected gorm.ErrRecordNotFound, got: %v", err)
		}

		if err := tdb1.Model(&Link{}).Where("id = ?", link1.ID).Updates(map[string]any{"org_id": 2}).Error; err == nil {
			t.Fatalf("expected error when updating org_id to 2 via TenantDB(1), got nil")
		}
	})

	t.Run("ConcurrentCrossTenantAccess_NoStateLeak", func(t *testing.T) {
		var wg sync.WaitGroup
		errCh := make(chan error, 40)

		for i := 0; i < 20; i++ {
			wg.Add(2)

			go func() {
				defer wg.Done()
				tdb := p.tenantDB(1)
				var links []Link
				if err := tdb.Find(&links).Error; err != nil {
					errCh <- err
					return
				}
				for _, l := range links {
					if l.OrgID != 1 {
						t.Errorf("tenant 1 read record belonging to org %d", l.OrgID)
					}
				}
			}()

			go func() {
				defer wg.Done()
				tdb := p.tenantDB(2)
				var links []Link
				if err := tdb.Find(&links).Error; err != nil {
					errCh <- err
					return
				}
				for _, l := range links {
					if l.OrgID != 2 {
						t.Errorf("tenant 2 read record belonging to org %d", l.OrgID)
					}
				}
			}()
		}

		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Errorf("concurrent query error: %v", err)
		}
	})
}
