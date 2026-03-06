package domain

import (
	"encoding/json"
	"testing"
)

func TestRedactField_TopLevel(t *testing.T) {
	data, _ := json.Marshal(map[string]any{
		"Name":  "Alice",
		"Email": "alice@example.com",
	})

	result, err := RedactField(data, "Email")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if m["Email"] != "[REDACTED]" {
		t.Errorf("expected Email=[REDACTED], got %v", m["Email"])
	}
	if m["Name"] != "Alice" {
		t.Errorf("Name should be preserved, got %v", m["Name"])
	}
}

func TestRedactField_Nested(t *testing.T) {
	data, _ := json.Marshal(map[string]any{
		"Address": map[string]any{
			"Street": "123 Main St",
			"City":   "Springfield",
		},
	})

	result, err := RedactField(data, "Address.Street")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	addr := m["Address"].(map[string]any)
	if addr["Street"] != "[REDACTED]" {
		t.Errorf("expected Street=[REDACTED], got %v", addr["Street"])
	}
	if addr["City"] != "Springfield" {
		t.Errorf("City should be preserved, got %v", addr["City"])
	}
}

func TestRedactField_MissingField(t *testing.T) {
	data, _ := json.Marshal(map[string]any{"Name": "Alice"})

	result, err := RedactField(data, "NonExistent")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}

	// Should return original data unchanged.
	var original, redacted map[string]any
	json.Unmarshal(data, &original)
	json.Unmarshal(result, &redacted)

	if original["Name"] != redacted["Name"] {
		t.Errorf("data should be unchanged when field not found")
	}
}
