package adapter

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func JSONNumber(v any) (*float64, bool) {
	switch value := v.(type) {
	case float64:
		return &value, true
	case json.Number:
		f, err := value.Float64()
		if err != nil {
			return nil, false
		}
		return &f, true
	case string:
		s := cleanNumeric(value)
		if s == "" || unknownValues[strings.ToLower(s)] {
			return nil, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, false
		}
		return &f, true
	default:
		return nil, false
	}
}

func JSONInt(v any) (*int, bool) {
	f, ok := JSONNumber(v)
	if !ok {
		return nil, false
	}
	n := int(*f)
	return &n, true
}

func JSONInt64(v any) (*int64, bool) {
	switch value := v.(type) {
	case json.Number:
		n, err := value.Int64()
		if err != nil {
			return nil, false
		}
		return &n, true
	case float64:
		n := int64(value)
		return &n, true
	case string:
		s := cleanNumeric(value)
		if s == "" || unknownValues[strings.ToLower(s)] {
			return nil, false
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			if f, ferr := strconv.ParseFloat(s, 64); ferr == nil {
				out := int64(f)
				return &out, true
			}
			return nil, false
		}
		return &n, true
	default:
		return nil, false
	}
}

func JSONString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func JSONBool(v any) (*bool, bool) {
	switch value := v.(type) {
	case bool:
		return &value, true
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "1", "yes":
			out := true
			return &out, true
		case "false", "0", "no":
			out := false
			return &out, true
		}
	}
	return nil, false
}

func JSONTime(v any, loc *time.Location) *time.Time {
	if loc == nil {
		loc = time.UTC
	}
	switch value := v.(type) {
	case string:
		if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && seconds > 0 {
			out := time.Unix(seconds, 0).UTC()
			return &out
		}
		if parsed, ok := ParseDate(value, loc); ok {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
			out := parsed.UTC()
			return &out
		}
	case json.Number:
		if seconds, err := value.Int64(); err == nil && seconds > 0 {
			out := time.Unix(seconds, 0).UTC()
			return &out
		}
	case float64:
		if value > 0 {
			out := time.Unix(int64(value), 0).UTC()
			return &out
		}
	}
	return nil
}

func DecodeJSON(body []byte, into any) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	return decoder.Decode(into)
}

func Map(m map[string]any, keys ...string) (any, bool) {
	var current any = m
	for _, key := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, exists := obj[key]
		if !exists {
			return nil, false
		}
		current = value
	}
	return current, true
}

func FirstMap(v any) (map[string]any, bool) {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil, false
	}
	obj, ok := arr[0].(map[string]any)
	return obj, ok
}
