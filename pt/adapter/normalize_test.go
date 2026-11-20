package adapter

import (
	"context"
	"testing"
	"time"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
		ok   bool
	}{
		{"1.5 TB", 1649267441664, true},
		{"2,048 GiB", 2199023255552, true},
		{"512 GB", 549755813888, true},
		{"100 MiB", 104857600, true},
		{"0 B", 0, true},
		{"1.5TiB", 1649267441664, true},
		{"", 0, false},
		{"—", 0, false},
		{"N/A", 0, false},
		{"未知", 0, false},
		{"∞", 0, false},
		{"-5 GiB", 0, false},
	}
	for _, tc := range cases {
		got, ok := ParseSize(tc.raw)
		if ok != tc.ok {
			t.Errorf("ParseSize(%q) ok = %v want %v", tc.raw, ok, tc.ok)
			continue
		}
		if ok && (got == nil || *got != tc.want) {
			t.Errorf("ParseSize(%q) = %v want %d", tc.raw, got, tc.want)
		}
	}
}

func TestParseRatio(t *testing.T) {
	if value, infinite, ok := ParseRatio("∞"); !ok || !infinite || value != nil {
		t.Fatalf("ParseRatio(∞) = %v,%v,%v", value, infinite, ok)
	}
	if value, infinite, ok := ParseRatio("1.234"); !ok || infinite || value == nil || *value != 1.234 {
		t.Fatalf("ParseRatio(1.234) = %v,%v,%v", value, infinite, ok)
	}
	if _, _, ok := ParseRatio("—"); ok {
		t.Fatal("a dash must not parse as a ratio")
	}
}

func TestParseDate(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	got, ok := ParseDate("2024-03-01 10:00:00", loc)
	if !ok || got == nil {
		t.Fatal("expected a parsed date")
	}
	if formatted := got.Format(time.RFC3339); formatted != "2024-03-01T02:00:00Z" {
		t.Fatalf("date = %s", formatted)
	}
	if _, ok := ParseDate("not a date", loc); ok {
		t.Fatal("garbage parsed as a date")
	}
	if _, ok := ParseDate("", loc); ok {
		t.Fatal("an empty value parsed as a date")
	}
}

func TestParseCount(t *testing.T) {
	if n, ok := ParseCount("1,234 个"); !ok || n == nil || *n != 1234 {
		t.Fatalf("ParseCount = %v,%v", n, ok)
	}
	if _, ok := ParseCount("N/A"); ok {
		t.Fatal("N/A parsed as a count")
	}
}

func TestNormalizeDomain(t *testing.T) {
	cases := map[string]string{
		"https://kp.m-team.cc/index.php":  "kp.m-team.cc",
		"HTTP://Tracker.Example.com:443/": "tracker.example.com",
		"www.example.com":                 "example.com",
		"":                                "",
	}
	for raw, want := range cases {
		if got := NormalizeDomain(raw); got != want {
			t.Errorf("NormalizeDomain(%q) = %q want %q", raw, got, want)
		}
	}
}

func TestRegistryRejectsDuplicateSchema(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(stubAdapter{schema: SchemaNexusPHP})
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate schema did not panic")
		}
	}()
	r.MustRegister(stubAdapter{schema: SchemaNexusPHP})
}

func TestRegistryGetUnknownIsUnsupported(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Get(SchemaMTeam); err == nil {
		t.Fatal("expected an error for an unknown schema")
	}
}

func TestRegistrySuggestUsesDomainHint(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(stubAdapter{schema: SchemaMTeam}, "kp.m-team.cc")
	if schema, ok := r.Suggest("https://kp.m-team.cc/index.php"); !ok || schema != SchemaMTeam {
		t.Fatalf("Suggest = %q,%v", schema, ok)
	}
	if _, ok := r.Suggest("https://unknown.example"); ok {
		t.Fatal("an unknown host must not be suggested")
	}
}

type stubAdapter struct{ schema Schema }

func (s stubAdapter) Schema() Schema             { return s.schema }
func (s stubAdapter) DisplayName() string        { return string(s.schema) }
func (s stubAdapter) Capabilities() Capabilities { return Capabilities{} }
func (s stubAdapter) Defaults() Defaults         { return Defaults{} }

func (s stubAdapter) Login(context.Context, *Session) (*LoginResult, error) {
	return &LoginResult{}, nil
}

func (s stubAdapter) CheckSession(context.Context, *Session) (*LoginResult, error) {
	return &LoginResult{}, nil
}

func (s stubAdapter) Signin(context.Context, *Session) (*SigninResult, error) {
	return &SigninResult{Outcome: OutcomeUnsupported}, nil
}

func (s stubAdapter) Stats(context.Context, *Session) (*UserStats, error) {
	return &UserStats{}, nil
}
