package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

const redactedValue = "[REDACTED]"

// RedactField replaces the value at fieldPath in the given JSON data with
// "[REDACTED]". The fieldPath uses dotted notation (e.g., "Email", "Address.Street").
// Returns the modified JSON data.
//
// If the field does not exist in the data, the original data is returned unchanged.
func RedactField(data []byte, fieldPath string) ([]byte, error) {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal for redaction: %w", err)
	}

	parts := strings.Split(fieldPath, ".")
	if redactNested(obj, parts) {
		result, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("marshal after redaction: %w", err)
		}
		return result, nil
	}

	// Field not found — return original data unchanged.
	return data, nil
}

// redactNested walks into nested maps following the path parts and replaces
// the leaf value with "[REDACTED]". Returns true if the field was found and redacted.
func redactNested(obj map[string]any, parts []string) bool {
	if len(parts) == 0 {
		return false
	}

	key := parts[0]

	if len(parts) == 1 {
		// Leaf: replace value.
		if _, exists := obj[key]; exists {
			obj[key] = redactedValue
			return true
		}
		return false
	}

	// Intermediate: recurse into nested map.
	nested, ok := obj[key].(map[string]any)
	if !ok {
		return false
	}
	return redactNested(nested, parts[1:])
}
