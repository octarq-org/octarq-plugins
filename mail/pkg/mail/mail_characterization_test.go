package mail

import (
	"strings"
	"testing"
)

func TestCharacterization_RFC822ParseLocks(t *testing.T) {
	raw := []byte("From: sender@example.com\r\n" +
		"To: receiver@example.com\r\n" +
		"Subject: Important Notification\r\n" +
		"Message-ID: <msg123@example.com>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Hello world!\r\n")

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.From != "sender@example.com" {
		t.Errorf("expected From sender@example.com, got %q", parsed.From)
	}
	if parsed.To != "receiver@example.com" {
		t.Errorf("expected To receiver@example.com, got %q", parsed.To)
	}
	if parsed.Subject != "Important Notification" {
		t.Errorf("expected Subject 'Important Notification', got %q", parsed.Subject)
	}
	if parsed.MessageID != "msg123@example.com" {
		t.Errorf("expected MessageID 'msg123@example.com', got %q", parsed.MessageID)
	}
	if !strings.Contains(parsed.Text, "Hello world!") {
		t.Errorf("expected Text to contain 'Hello world!', got %q", parsed.Text)
	}
}

func TestCharacterization_UnparseableFallback(t *testing.T) {
	corruptRaw := []byte{0x00, 0xFF, 0xFE, 0xFD}
	parsed, err := Parse(corruptRaw)
	if err != nil {
		t.Fatalf("expected non-error fallback for corrupt raw mail, got: %v", err)
	}
	if parsed == nil {
		t.Fatal("expected non-nil parsed record")
	}
	if len(parsed.Raw) != len(corruptRaw) {
		t.Errorf("expected raw bytes preserved")
	}
}

func TestCharacterization_StripCRLFInjection(t *testing.T) {
	maliciousInput := "victim@example.com\r\nBcc: hacker@example.com\nSubject: Injected"
	cleaned := stripCRLF(maliciousInput)

	if strings.Contains(cleaned, "\r") || strings.Contains(cleaned, "\n") {
		t.Fatalf("stripCRLF failed to strip newline characters: %q", cleaned)
	}
	if cleaned != "victim@example.comBcc: hacker@example.comSubject: Injected" {
		t.Errorf("unexpected stripCRLF output: %q", cleaned)
	}
}

func TestCharacterization_SenderFailClosedLocks(t *testing.T) {
	s := NewCustomSender("127.0.0.1", "25", "user", "pass", "sender@example.com")
	if s == nil {
		t.Fatal("NewSender returned nil")
	}

	msg := Message{
		From:    "sender@example.com",
		To:      []string{"rec@example.com"},
		Subject: "Test",
		Text:    "Test message",
	}

	err := s.Send(msg)
	if err == nil {
		t.Fatal("expected error connecting to inactive port")
	}
}
