package registry

import (
	"reflect"
	"strings"
)

// parseFieldConfig extracts a [FieldConfig] from a struct field, parsing
// the version:"..." tag into its constituent flags.
//
// Supported tag values (comma-separated):
//   - id         — entity identifier
//   - tracked    — explicit opt-in for diffing
//   - ignore     — explicitly excluded from diffing
//   - normalized — sort slices before comparison
//   - redactable — informational flag for audit redaction
func parseFieldConfig(f reflect.StructField, index int) FieldConfig {
	fc := FieldConfig{
		Name:  f.Name,
		Index: index,
		Type:  f.Type,
	}

	tag := f.Tag.Get("version")
	if tag == "" {
		return fc
	}

	for _, part := range strings.Split(tag, ",") {
		switch strings.TrimSpace(part) {
		case "id":
			fc.ID = true
		case "tracked":
			fc.Tracked = true
		case "ignore":
			fc.Ignore = true
		case "normalized":
			fc.Normalized = true
		case "redactable":
			fc.Redactable = true
		}
	}

	return fc
}
