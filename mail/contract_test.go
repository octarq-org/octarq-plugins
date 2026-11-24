package mail

import (
	"encoding/json"
	"testing"
	"time"
)

func TestInboundEmailWireJSON(t *testing.T) {
	e := InboundEmail{
		ID:         7,
		MailboxID:  8,
		OrgID:      3,
		From:       "sender@example.com",
		To:         "user@example.net",
		Subject:    "Hello",
		Text:       "plain body",
		HTML:       "rich body",
		ReceivedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	got, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":7,"mailboxId":8,"orgId":3,"from":"sender@example.com","to":"user@example.net","subject":"Hello","text":"plain body","html":"rich body","receivedAt":"2026-01-02T03:04:05Z"}`
	if string(got) != want {
		t.Errorf("mail.inbound payload\n got: %s\nwant: %s", got, want)
	}
}

func TestWireMetricLiterals(t *testing.T) {
	if metricMailIn != "mailIn" {
		t.Errorf("metricMailIn = %q, want %q", metricMailIn, "mailIn")
	}
	if metricMailOut != "mailOut" {
		t.Errorf("metricMailOut = %q, want %q", metricMailOut, "mailOut")
	}
	if metricRawBytes != "mail.raw_bytes" {
		t.Errorf("metricRawBytes = %q, want %q", metricRawBytes, "mail.raw_bytes")
	}
	if ServiceStorageProvider != "storage.provider" {
		t.Errorf("ServiceStorageProvider = %q, want %q", ServiceStorageProvider, "storage.provider")
	}
}
