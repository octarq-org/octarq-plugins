package links

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/plugin"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Link{}, &LinkEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func testEngine(t *testing.T, publishEvent func(uint, string, any)) *Engine {
	t.Helper()
	ctx := &plugin.Context{
		Host: &plugin.TestHost{
			EventsMock: &plugin.TestEvents{
				PublishEventFn: publishEvent,
			},
		},
	}
	e := &Engine{
		db:          testDB(t),
		ctx:         ctx,
		host:        plugin.EnsureHost(ctx),
		queue:       make(chan clickItem, 100),
		rateLimiter: newIPRateLimiter(300, time.Minute),
	}
	e.wg.Add(1)
	go e.worker()
	return e
}

func TestWorkerSurvivesPanic(t *testing.T) {
	var panicOnce sync.Once
	received := make(chan uint, 10)

	e := testEngine(t, func(orgID uint, _ string, _ any) {
		panicOnce.Do(func() {
			panic("simulated PublishEvent panic")
		})
		received <- orgID
	})

	e.db.Create(&Link{ID: 1, Slug: "a", Enabled: true})
	e.db.Create(&Link{ID: 2, Slug: "b", Enabled: true})

	e.queue <- clickItem{orgID: 1, linkID: 1, createdAt: time.Now()}

	time.Sleep(500 * time.Millisecond)

	e.queue <- clickItem{orgID: 2, linkID: 2, createdAt: time.Now()}

	select {
	case id := <-received:
		if id != 2 {
			t.Fatalf("expected orgID 2, got %d", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not resume after panic — second item was never consumed")
	}

	e.Close()
}

func TestWorkerCloseAfterPanic(t *testing.T) {
	var calls atomic.Int32

	e := testEngine(t, func(_ uint, _ string, _ any) {
		if calls.Add(1) == 1 {
			panic("boom")
		}
	})

	e.db.Create(&Link{ID: 1, Slug: "c", Enabled: true})

	e.queue <- clickItem{orgID: 1, linkID: 1, createdAt: time.Now()}
	time.Sleep(500 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		e.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close() blocked — the worker restart loop did not honor channel close")
	}
}
