package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DecodePaths accepts the E4 dual form of `paths`: a native string array or
// the legacy JSON-array string adapter. The two cannot appear as separate
// fields (same parameter name).
func DecodePaths(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return nil, nil
		}
		var paths []string
		if err := json.Unmarshal([]byte(t), &paths); err != nil {
			return nil, fmt.Errorf(`parameter "paths": expected a native array or JSON array of strings: %w`, err)
		}
		return paths, nil
	case []string:
		out := make([]string, len(t))
		copy(out, t)
		return out, nil
	case []interface{}:
		out := make([]string, 0, len(t))
		for i, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf(`parameter "paths": item %d is %T, want string`, i, item)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf(`parameter "paths": expected array or JSON string, got %T`, v)
	}
}

// DecodeDual decodes a native JSON value or its *_json string adapter.
// Both forms together are accepted only when they decode to the same value.
func DecodeDual[T any](native any, jsonStr, nativeField, jsonField string) (T, error) {
	var zero T
	out, ok, err := DecodeDualOptional[T](native, jsonStr, nativeField, jsonField)
	if err != nil {
		return zero, err
	}
	if !ok {
		return zero, fmt.Errorf("%s or %s is required", nativeField, jsonField)
	}
	return out, nil
}

// DecodeDualOptional is DecodeDual when neither form is required.
func DecodeDualOptional[T any](native any, jsonStr, nativeField, jsonField string) (T, bool, error) {
	var zero T
	hasNative := native != nil
	hasJSON := strings.TrimSpace(jsonStr) != ""
	parseJSON := func() (T, error) {
		var out T
		if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
			return zero, fmt.Errorf(`parameter %q: invalid JSON: %v (fix the JSON or pass native %s)`, jsonField, err, nativeField)
		}
		return out, nil
	}
	parseNative := func() (T, error) {
		b, err := json.Marshal(native)
		if err != nil {
			return zero, fmt.Errorf(`parameter %q: %v`, nativeField, err)
		}
		var out T
		if err := json.Unmarshal(b, &out); err != nil {
			return zero, fmt.Errorf(`parameter %q: %v (expected the same shape as %s)`, nativeField, err, jsonField)
		}
		return out, nil
	}
	switch {
	case hasNative && hasJSON:
		n, err := parseNative()
		if err != nil {
			return zero, false, err
		}
		j, err := parseJSON()
		if err != nil {
			return zero, false, err
		}
		if !sameJSON(n, j) {
			return zero, false, fmt.Errorf("conflicting %s and %s; provide only one form or make them identical", nativeField, jsonField)
		}
		return n, true, nil
	case hasNative:
		n, err := parseNative()
		return n, err == nil, err
	case hasJSON:
		j, err := parseJSON()
		return j, err == nil, err
	default:
		return zero, false, nil
	}
}

func sameJSON(a, b any) bool {
	sa, err := jsonCanon(a)
	if err != nil {
		return false
	}
	sb, err := jsonCanon(b)
	if err != nil {
		return false
	}
	return sa == sb
}

func jsonCanon(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var norm any
	if err := json.Unmarshal(b, &norm); err != nil {
		return "", err
	}
	out, err := json.Marshal(norm)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
