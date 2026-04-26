package session

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// argsMap is a simple map of parsed JSON arguments.
type argsMap map[string]interface{}

// parseArgs parses a JSON string into an argsMap.
func parseArgs(raw string) argsMap {
	if raw == "" {
		return argsMap{}
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return argsMap{}
	}
	return parsed
}

// String returns a string value from the map, or default.
func (a argsMap) String(key, def string) string {
	v, ok := a[key]
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	return s
}

// Int returns an int value from the map, or default.
func (a argsMap) Int(key string, def int) int {
	v, ok := a[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return def
}

// StringSlice returns a string slice value from the map.
func (a argsMap) StringSlice(key string) []string {
	v, ok := a[key]
	if !ok {
		return nil
	}
	switch s := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	case []string:
		return s
	}
	return nil
}

// extractExitCode extracts the exit_code from a terminal output JSON.
func extractExitCode(content string) string {
	re := regexp.MustCompile(`"exit_code"\s*:\s*(-?\d+)`)
	match := re.FindStringSubmatch(content)
	if len(match) > 1 {
		return match[1]
	}
	return "?"
}

// extractJSONInt extracts an integer value from a JSON-like content string.
func extractJSONInt(content, key string) int {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*(\d+)`)
	match := re.FindStringSubmatch(content)
	if len(match) > 1 {
		if n, err := strconv.Atoi(match[1]); err == nil {
			return n
		}
	}
	return 0
}

// tryParseJSON attempts to parse a JSON string, returning nil on failure.
func tryParseJSON(s string) (interface{}, error) {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	return v, nil
}

// marshalJSON marshals a value to JSON string with no escaping of special chars.
func marshalJSON(v interface{}) (string, error) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// shrinkStringsInValue recursively shrinks long string values in a parsed JSON structure.
func shrinkStringsInValue(v interface{}, headChars int) interface{} {
	switch val := v.(type) {
	case string:
		if len(val) > headChars {
			return val[:headChars] + "...[truncated]"
		}
		return val
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, iv := range val {
			result[k] = shrinkStringsInValue(iv, headChars)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, iv := range val {
			result[i] = shrinkStringsInValue(iv, headChars)
		}
		return result
	default:
		return v
	}
}
