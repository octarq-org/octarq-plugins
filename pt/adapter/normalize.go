package adapter

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

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

func ParseDate(raw string, loc *time.Location) (*time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || unknownValues[strings.ToLower(s)] {
		return nil, false
	}
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

func cleanNumeric(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", "")
	return s
}

func TruncateEvidence(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if max <= 0 || len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
