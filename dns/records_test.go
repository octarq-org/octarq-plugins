package dns

import (
	"testing"

	"github.com/octarq-org/octarq-plugins/dns/contract"
	"github.com/octarq-org/octarq-plugins/dns/provider"
)

func TestValidateRecord(t *testing.T) {
	p := 10
	cases := []struct {
		name string
		rec  provider.Record
		ok   bool
	}{
		{"valid A", provider.Record{Type: "A", Content: "1.2.3.4"}, true},
		{"missing type", provider.Record{Content: "1.2.3.4"}, false},
		{"empty content", provider.Record{Type: "A", Content: ""}, false},
		{"MX without priority", provider.Record{Type: "MX", Content: "mx.x.com"}, false},
		{"MX with priority", provider.Record{Type: "MX", Content: "mx.x.com", Priority: &p}, true},
		{"lowercase mx without priority", provider.Record{Type: "mx", Content: "mx.x.com"}, false},
	}
	for _, c := range cases {
		msg := validateRecord(c.rec)
		if (msg == "") != c.ok {
			t.Errorf("%s: validateRecord = %q, wantOK=%v", c.name, msg, c.ok)
		}
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"  GO.Example.com/ ":    "go.example.com",
		"https://s.example.com": "s.example.com",
		"http://x.com/path":     "x.com",
		"a.com.":                "a.com",
	}
	for in, want := range cases {
		if got := normalizeHost(in); got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDNSRecordMappingRoundTrip(t *testing.T) {
	prio := 10
	in := provider.Record{
		ID: "r1", Type: "MX", Name: "example.com", Content: "mail.example.com",
		TTL: 600, Proxied: true, Comment: "note", Priority: &prio,
	}
	pr := toContractRecord(in)
	if pr.ID != in.ID || pr.Type != in.Type || pr.Name != in.Name || pr.Content != in.Content ||
		pr.TTL != in.TTL || pr.Proxied != in.Proxied || pr.Comment != in.Comment ||
		pr.Priority == nil || *pr.Priority != prio {
		t.Errorf("toContractRecord lost data: %+v", pr)
	}
	back := fromContractRecord(pr)
	if back != in {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", back, in)
	}
	if fromContractRecord(contract.Record{Type: "A"}).ID != "" {
		t.Error("empty ID should round-trip to empty (create)")
	}
}
