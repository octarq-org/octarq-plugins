package adapter

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// unknownValues are the tokens sites use for "no data". They must parse as
// unknown, not as zero.
var unknownValues = map[string]bool{
	"":        true,
	"-":       true,
	"--":      true,
	"—":       true,
	"--—":     true,
	"n/a":     true,
	"na":      true,
	"nan":     true,
	"null":    true,
	"none":    true,
	"unknown": true,
	"?":       true,
	"未知":      true,
	"无":       true,
	"暂无":      true,
	"無限":      true,
	"无限":      true,
}

// ParseSize parses a human size into bytes using 1024-based units. It accepts
// both the KB and KiB spellings, because sites use them interchangeably.
//
// ok is false when raw is not a size (empty, a dash, "N/A", "未知"). A valid
// zero returns a pointer to 0 and ok true: "0 download" is real data.
func ParseSize(raw string) (*int64, bool) {
	s := cleanNumeric(raw)
	if unknownValues[strings.ToLower(s)] {
		return nil, false
	}

	idx := 0
	for idx < len(s) {
		ch := s[idx]
		if (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '+' {
			idx++
			continue
		}
		break
	}
	numPart, unitPart := s[:idx], strings.ToUpper(strings.TrimSpace(s[idx:]))
	if numPart == "" {
		return nil, false
	}

	value, err := strconv.ParseFloat(numPart, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil, false
	}

	mult, ok := sizeMultiplier(unitPart)
	if !ok {
		return nil, false
	}
	bytesF := value * float64(mult)
	if bytesF > math.MaxInt64 {
		return nil, false
	}
	out := int64(math.Round(bytesF))
	return &out, true
}

func sizeMultiplier(unit string) (int64, bool) {
	unit = strings.TrimSuffix(strings.TrimSpace(unit), "S")
	unit = strings.TrimSuffix(unit, "B")
	unit = strings.TrimSuffix(unit, "I")
	switch unit {
	case "":
		return 1, true
	case "K":
		return 1 << 10, true
	case "M":
		return 1 << 20, true
	case "G":
		return 1 << 30, true
	case "T":
		return 1 << 40, true
	case "P":
		return 1 << 50, true
	case "E":
		return 1 << 60, true
	}
	return 0, false
}

// ParseRatio parses a share ratio. infinite is true for the site's "∞" marker;
// value is nil in that case because there is no finite number to store.
func ParseRatio(raw string) (value *float64, infinite bool, ok bool) {
	s := cleanNumeric(raw)
	lower := strings.ToLower(s)
	if lower == "∞" || lower == "inf" || lower == "infinite" || lower == "无限" || lower == "無限" {
		return nil, true, true
	}
	if unknownValues[lower] {
		return nil, false, false
	}

	num, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	if err != nil || math.IsNaN(num) || math.IsInf(num, 0) || num < 0 {
		return nil, false, false
	}
	return &num, false, true
}

// ParseCount parses a non-negative integer, ignoring thousands separators and
// a trailing unit word.
func ParseCount(raw string) (*int, bool) {
	s := cleanNumeric(raw)
	if unknownValues[strings.ToLower(s)] {
		return nil, false
	}

	var digits strings.Builder
	started := false
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits.WriteRune(r)
			started = true
		case started:
			// Stop at the first non-digit after the number started; the rest
			// is a unit or annotation.
			goto parsed
		}
	}
parsed:
	if digits.Len() == 0 {
		return nil, false
	}
	n, err := strconv.Atoi(digits.String())
	if err != nil {
		return nil, false
	}
	return &n, true
}

// dateLayouts are tried in order. Sites mix separators and sometimes drop the
// time part.
var dateLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006/01/02",
	"2006.01.02 15:04:05",
	"2006.01.02",
	"2006-1-2 15:04:05",
	"2006-1-2",
	"Jan 02 2006 15:04:05",
	"Jan 02 2006",
	"02 Jan 2006",
	time.RFC3339,
}

// ParseDate parses a site date in the site's timezone. An empty or unknown
// value returns ok false rather than the zero time.
func ParseDate(raw string, loc *time.Location) (*time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || unknownValues[strings.ToLower(s)] {
		return nil, false
	}
	// Trailing annotations such as "(3 years ago)" are noise.
	if i := strings.Index(s, "("); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	if loc == nil {
		loc = time.Local
	}

	for _, layout := range dateLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			utc := t.UTC()
			return &utc, true
		}
	}
	return nil, false
}

// cleanNumeric normalizes spacing and thousands separators.
func cleanNumeric(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", "")
	return s
}

// TruncateEvidence keeps a short, single-line excerpt for the sign-in ledger.
// It never carries secrets because callers pass visible page text, not
// requests or cookies.
func TruncateEvidence(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if max <= 0 || len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
