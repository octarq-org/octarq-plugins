package links

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type testOrg struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"size:255"`
}

func (testOrg) TableName() string { return "orgs" }

type testSetting struct {
	Key   string `gorm:"primaryKey;size:255"`
	Value string `gorm:"type:text"`
}

func (testSetting) TableName() string { return "settings" }

type testDomain struct {
	ID                uint   `gorm:"primaryKey"`
	OrgID             uint   `gorm:"column:owner_id;index;default:1"`
	Name              string `gorm:"uniqueIndex;size:255"`
	ProviderAccountID uint   `gorm:"index"`
	ZoneID            string `gorm:"size:64"`
	Note              string `gorm:"type:text"`
	ForMail           bool
	ForLink           bool
	LinkHosts         hostlist.HostList `gorm:"type:text"`
	MailHosts         hostlist.HostList `gorm:"type:text"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (testDomain) TableName() string { return "domains" }

func (d testDomain) EffectiveLinkHosts() []string { return d.LinkHosts.Enabled() }

type testProviderAccount struct {
	ID    uint   `gorm:"primaryKey"`
	OrgID uint   `gorm:"column:owner_id"`
	Name  string `gorm:"size:255"`
}

func (testProviderAccount) TableName() string { return "provider_accounts" }

type testMemoryCache struct {
	mu   sync.RWMutex
	data map[string]any
}

func newTestMemoryCache() *testMemoryCache {
	return &testMemoryCache{data: make(map[string]any)}
}

func (c *testMemoryCache) Get(ctx context.Context, key string, dest any) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.data[key]
	if !ok {
		return false
	}
	if pLink, ok := dest.(*Link); ok {
		if stored, ok := val.(*Link); ok {
			*pLink = *stored
			return true
		}
	}
	return false
}

func (c *testMemoryCache) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if link, ok := val.(*Link); ok {
		cp := *link
		c.data[key] = &cp
	} else {
		c.data[key] = val
	}
	return nil
}

func (c *testMemoryCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
	return nil
}

func setupLinksTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("setupLinksTestDB: %v", err)
	}
	if err := db.AutoMigrate(
		&Link{},
		&LinkEvent{},
		&testOrg{},
		&testSetting{},
		&testDomain{},
		&testProviderAccount{},
	); err != nil {
		t.Fatalf("setupLinksTestDB migrate: %v", err)
	}
	return db
}
