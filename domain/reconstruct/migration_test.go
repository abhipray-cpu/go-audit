package reconstruct

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
)

func TestRegisterMigration_V1toV2(t *testing.T) {
	reg := NewMigrationRegistry()

	// Register a v1→v2 migration that adds a "full_name" field.
	err := reg.Register("user", 1, 2, func(_ context.Context, data []byte) ([]byte, error) {
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		first, _ := m["first_name"].(string)
		last, _ := m["last_name"].(string)
		m["full_name"] = first + " " + last
		return json.Marshal(m)
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Simulate a v1 record.
	v1Data, _ := json.Marshal(map[string]any{
		"first_name": "John",
		"last_name":  "Doe",
	})

	// Migrate from schema v1 to v2.
	v2Data, version, err := reg.Migrate(context.Background(), "user", v1Data, 1, 2)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if version != 2 {
		t.Fatalf("expected schema version 2, got %d", version)
	}

	var result map[string]any
	if err := json.Unmarshal(v2Data, &result); err != nil {
		t.Fatalf("Unmarshal v2: %v", err)
	}
	if result["full_name"] != "John Doe" {
		t.Errorf("expected full_name=John Doe, got %v", result["full_name"])
	}
}

func TestRegisterMigration_GapRejected(t *testing.T) {
	reg := NewMigrationRegistry()

	// v1→v3 without v2 must fail.
	err := reg.Register("user", 1, 3, func(_ context.Context, data []byte) ([]byte, error) {
		return data, nil
	})
	if err == nil {
		t.Fatal("expected error for gap v1→v3, got nil")
	}
	if !isErr(err, domain.ErrMigration) {
		t.Errorf("expected ErrMigration, got %v", err)
	}
}

func TestSchemaVersion_AutoBump(t *testing.T) {
	reg := NewMigrationRegistry()

	// No migrations → latest is 1.
	if v := reg.LatestSchemaVersion("user"); v != 1 {
		t.Fatalf("expected latest=1 before migrations, got %d", v)
	}

	// Register v1→v2.
	noop := func(_ context.Context, data []byte) ([]byte, error) { return data, nil }
	if err := reg.Register("user", 1, 2, noop); err != nil {
		t.Fatalf("Register v1→v2: %v", err)
	}
	if v := reg.LatestSchemaVersion("user"); v != 2 {
		t.Fatalf("expected latest=2, got %d", v)
	}

	// Register v2→v3.
	if err := reg.Register("user", 2, 3, noop); err != nil {
		t.Fatalf("Register v2→v3: %v", err)
	}
	if v := reg.LatestSchemaVersion("user"); v != 3 {
		t.Fatalf("expected latest=3, got %d", v)
	}
}

func TestSchemaEvolution_5VersionChain(t *testing.T) {
	reg := NewMigrationRegistry()
	ctx := context.Background()

	// Register 4 migrations: v1→v2→v3→v4→v5.
	for from := 1; from <= 4; from++ {
		fromCopy := from
		err := reg.Register("user", from, from+1, func(_ context.Context, data []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, err
			}
			// Each migration adds a field "step_N".
			m[stepKey(fromCopy)] = fromCopy
			return json.Marshal(m)
		})
		if err != nil {
			t.Fatalf("Register v%d→v%d: %v", from, from+1, err)
		}
	}

	// Start with a v1 entity.
	v1Data, _ := json.Marshal(map[string]any{"id": "u1"})

	// Migrate all the way from v1 to v5.
	v5Data, version, err := reg.Migrate(ctx, "user", v1Data, 1, 5)
	if err != nil {
		t.Fatalf("Migrate v1→v5: %v", err)
	}
	if version != 5 {
		t.Fatalf("expected schema version 5, got %d", version)
	}

	var result map[string]any
	if err := json.Unmarshal(v5Data, &result); err != nil {
		t.Fatalf("Unmarshal v5: %v", err)
	}

	// Each step should have added its field.
	for from := 1; from <= 4; from++ {
		key := stepKey(from)
		val, ok := result[key]
		if !ok {
			t.Errorf("missing key %q after migration chain", key)
			continue
		}
		// JSON numbers unmarshal as float64.
		if int(val.(float64)) != from {
			t.Errorf("expected %s=%d, got %v", key, from, val)
		}
	}
}

func stepKey(n int) string {
	return "step_" + string(rune('0'+n))
}

// isErr checks if target is in err's chain.
func isErr(err, target error) bool {
	return err != nil && (err == target || containsErr(err, target))
}

func containsErr(err, target error) bool {
	for e := err; e != nil; {
		if e == target {
			return true
		}
		if u, ok := e.(interface{ Unwrap() error }); ok {
			e = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}
