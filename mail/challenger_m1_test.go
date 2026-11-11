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

// TestChallengerM1_MIMEParserFuzz stress-tests the RFC822 MIME parser with thousands
// of randomized malformed, truncated, deeply nested, and corrupted MIME payloads.
// It verifies that Parse NEVER panics, NEVER hangs, and gracefully falls back.
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
		case 0: // Truncation
			cut := r.Intn(len(base))
			payload = []byte(base[:cut])
		case 1: // Null byte injection
			b := []byte(base)
			pos := r.Intn(len(b))
			b[pos] = 0
			payload = b
		case 2: // Giant header injection
			giant := strings.Repeat("X-Spam-Test: "+strings.Repeat("A", 1000)+"\r\n", 10)
			payload = []byte(giant + base)
		case 3: // Recursive / broken boundary markers
			broken := strings.ReplaceAll(base, "frontier", "frontier_"+strings.Repeat("-", r.Intn(50)))
			payload = []byte(broken)
		case 4: // Random byte corruption
			b := []byte(base)
			for j := 0; j < r.Intn(20); j++ {
				b[r.Intn(len(b))] = byte(r.Intn(256))
			}
			payload = b
		case 5: // Deeply nested multipart bomb attempt
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

		// Must never panic or hang
		parsed, err := mail.Parse(payload)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error returned: %v", i, err)
		}
		if parsed == nil {
			t.Fatalf("iteration %d: parsed result must never be nil", i)
		}
	}
}

// TestChallengerM1_OutboundSMTSSRFGuard verifies that SMTPSender refuses outbound connections
// to private, loopback, and metadata network IP addresses at dial time.
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

// TestChallengerM1_DBStorageProviderConcurrency stress-tests DBStorageProvider under
// concurrent Put, Get, Stat, and Delete operations to ensure thread safety and data integrity.
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

			// Put
			if err := provider.Put(ctx, key, data); err != nil {
				t.Errorf("worker %d: Put failed: %v", id, err)
				return
			}
			putCount.Add(1)

			// Stat
			size, err := provider.Stat(ctx, key)
			if err != nil || size != int64(len(data)) {
				t.Errorf("worker %d: Stat failed (size=%d, want=%d, err=%v)", id, size, len(data), err)
			}

			// Get
			fetched, err := provider.Get(ctx, key)
			if err != nil || string(fetched) != string(data) {
				t.Errorf("worker %d: Get mismatch or error: %v", id, err)
				return
			}
			getCount.Add(1)

			// Delete half
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
