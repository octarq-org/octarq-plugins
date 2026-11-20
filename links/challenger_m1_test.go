package links

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestChallengerM1_ConcurrentLinkCreationProgrammatic(t *testing.T) {
	db := setupLinksTestDB(t)

	p := &Plugin{
		db: db,
	}

	const workers = 50
	var wg sync.WaitGroup
	wg.Add(workers)

	var successCount atomic.Int32
	var failCount atomic.Int32
	var generatedSlugs sync.Map

	ctx := context.Background()

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			slug, err := p.CreateLink(ctx, 1, fmt.Sprintf("https://example.com/dest-%d", id))
			if err != nil {
				failCount.Add(1)
				t.Errorf("worker %d: unexpected CreateLink error: %v", id, err)
			} else if slug != "" {
				successCount.Add(1)
				if _, loaded := generatedSlugs.LoadOrStore(slug, true); loaded {
					t.Errorf("worker %d: duplicate slug generated: %s", id, slug)
				}
			}
		}(i)
	}
	wg.Wait()

	t.Logf("CreateLink concurrent results: success=%d, failed=%d", successCount.Load(), failCount.Load())

	if successCount.Load() != workers {
		t.Fatalf("expected all %d creations to succeed, got %d", workers, successCount.Load())
	}

	var count int64
	if err := db.Model(&Link{}).Where("owner_id = ?", 1).Count(&count).Error; err != nil {
		t.Fatalf("count links: %v", err)
	}
	if count != workers {
		t.Fatalf("expected %d rows in DB, found %d", workers, count)
	}
}

func TestChallengerM1_CreateLinkMaliciousTargets(t *testing.T) {
	db := setupLinksTestDB(t)
	p := &Plugin{
		db: db,
	}

	maliciousTargets := []string{
		"javascript:alert(1)",
		"javascript://alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
		"ftp://example.com/file",
		"",
	}

	for _, target := range maliciousTargets {
		_, err := p.CreateLink(context.Background(), 1, target)
		if err == nil {
			t.Errorf("expected error for malicious target %q, got nil", target)
		}
	}

	_, err := p.CreateLink(context.Background(), 0, "https://example.com")
	if err == nil {
		t.Error("expected error for orgID=0, got nil")
	}
}

func TestChallengerM1_ClickEngineQueueStress(t *testing.T) {
	eng, db := newTestPipelineEngine(t)
	defer eng.Close()

	link := &Link{
		OrgID:   1,
		Slug:    "concurrent-stress-target",
		Target:  "https://octarq.org/welcome",
		Enabled: true,
	}
	if err := db.Create(link).Error; err != nil {
		t.Fatalf("create link: %v", err)
	}

	const nRequests = 100
	var wg sync.WaitGroup
	wg.Add(nRequests)

	for i := 0; i < nRequests; i++ {
		go func(id int) {
			defer wg.Done()
			eng.queue <- clickItem{
				orgID:     1,
				slug:      link.Slug,
				linkID:    link.ID,
				ip:        fmt.Sprintf("203.0.113.%d", (id%200)+1),
				ua:        fmt.Sprintf("StressBot/%d", id),
				createdAt: time.Now(),
			}
		}(i)
	}
	wg.Wait()
}
