package mail

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/octarq-org/octarq-plugins/mail/pkg/mail"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestChallengerM1_MIMEParserFuzz(t *testing.T) {
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(orig)
	seeds := []string{
		"From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Simple\r\n\r\nHello World",
		"Content-Type: multipart/mixed; boundary=\"frontier\"\r\n\r\n--frontier\r\nContent-Type: text/plain\r\n\r\nPart 1\r\n--frontier\r\nContent-Type: text/html\r\n\r\n<b>Part 2</b>\r\n--frontier--",
		"Subject: =?UTF-8?B?4pyTIMOgIGxhIG1vZGU=?=\r\n\r\nUnicode subject",
		"From: \"Bad <Angle>\" <user@domain.com>\r\nTo: recipient@domain.com\r\nSubject: Test\r\n\r\nBody",
	}

	r := rand.New(rand.NewSource(1337))

	const iterations = 2000
	for i := 0; i < iterations; i++ {
		base := seeds[r.Intn(len(seeds))]
		var payload []byte

		switch r.Intn(6) {
		case 0:
			cut := r.Intn(len(base))
			payload = []byte(base[:cut])
		case 1:
			b := []byte(base)
			pos := r.Intn(len(b))
			b[pos] = 0
			payload = b
		case 2:
			giant := strings.Repeat("X-Spam-Test: "+strings.Repeat("A", 1000)+"\r\n", 10)
			payload = []byte(giant + base)
		case 3:
			broken := strings.ReplaceAll(base, "frontier", "frontier_"+strings.Repeat("-", r.Intn(50)))
			payload = []byte(broken)
		case 4:
			b := []byte(base)
			for j := 0; j < r.Intn(20); j++ {
				b[r.Intn(len(b))] = byte(r.Intn(256))
			}
			payload = b
		case 5:
			var nest strings.Builder
			for depth := 0; depth < 30; depth++ {
				nest.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"b%d\"\r\n\r\n--b%d\r\n", depth, depth))
			}
			nest.WriteString("Content-Type: text/plain\r\n\r\nDeep content\r\n")
			for depth := 29; depth >= 0; depth-- {
				nest.WriteString(fmt.Sprintf("--b%d--\r\n", depth))
			}
			payload = []byte(nest.String())
		}

		parsed, err := mail.Parse(payload)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error returned: %v", i, err)
		}
		if parsed == nil {
			t.Fatalf("iteration %d: parsed result must never be nil", i)
		}
	}
}

func TestChallengerM1_OutboundSMTSSRFGuard(t *testing.T) {
	ssrfTargets := []string{
		"127.0.0.1",
		"localhost",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.169.254",
		"::1",
	}

	for _, target := range ssrfTargets {
		sender := mail.NewCustomSender(target, "25", "", "", "attacker@example.com")
		err := sender.Send(mail.Message{
			To:      []string{"victim@example.com"},
			Subject: "SSRF Probe",
			Text:    "Testing dial time security",
		})
		if err == nil {
			t.Errorf("expected SSRF dial error for target %q, got nil", target)
		}
	}
}

func TestChallengerM1_DBStorageProviderConcurrency(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&MailRawBlob{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	provider := NewDBStorageProvider(db)
	ctx := context.Background()

	const workers = 40
	var wg sync.WaitGroup
	wg.Add(workers)

	var putCount atomic.Int32
	var getCount atomic.Int32

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("tenant-1/msg-%d", id)
			data := []byte(fmt.Sprintf("RFC822 raw message body content for id %d", id))

			if err := provider.Put(ctx, key, data); err != nil {
				t.Errorf("worker %d: Put failed: %v", id, err)
				return
			}
			putCount.Add(1)

			size, err := provider.Stat(ctx, key)
			if err != nil || size != int64(len(data)) {
				t.Errorf("worker %d: Stat failed (size=%d, want=%d, err=%v)", id, size, len(data), err)
			}

			fetched, err := provider.Get(ctx, key)
			if err != nil || string(fetched) != string(data) {
				t.Errorf("worker %d: Get mismatch or error: %v", id, err)
				return
			}
			getCount.Add(1)

			if id%2 == 0 {
				_ = provider.Delete(ctx, key)
			}
		}(i)
	}
	wg.Wait()

	t.Logf("Storage concurrency test completed: put=%d, get=%d", putCount.Load(), getCount.Load())
	if putCount.Load() != workers || getCount.Load() != workers {
		t.Fatalf("expected all workers to complete Put and Get")
	}
}
