package connector

import "strconv"

// IntFromProps extracts an int from a connector properties map, accepting any
// reasonable representation. The HCL parser stores numbers as int / int64 /
// float64, but values sourced from env() are always strings — so a numeric
// string must coerce successfully too. Anything else (or a missing key) falls
// back to defaultVal.
//
// The silent fallback is intentional: factories use this for optional config
// where the default is correct. Use IntFromPropsStrict when you need the
// caller to surface coercion errors.
func IntFromProps(props map[string]interface{}, key string, defaultVal int) int {
	n, ok := intFromProps(props, key)
	if !ok {
		return defaultVal
	}
	return n
}

// IntFromPropsStrict returns (value, ok). ok is false when the key is absent
// or the value cannot be coerced to int — useful when the caller wants to
// distinguish "not set" from "set to a default".
func IntFromPropsStrict(props map[string]interface{}, key string) (int, bool) {
	return intFromProps(props, key)
}

func intFromProps(props map[string]interface{}, key string) (int, bool) {
	if props == nil {
		return 0, false
	}
	v, ok := props[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		if n == "" {
			return 0, false
		}
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0, false
		}
		return i, true
	}
	return 0, false
}
