//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/reconstruct"
)

// ---------------------------------------------------------------------------
// Phase 2 — SE-001 to SE-005: Schema Evolution (RegisterMigration)
// ---------------------------------------------------------------------------

// SE-001: TestSchemaEvolution_RegisterMigration registers a v1→v2 migration.
func TestSchemaEvolution_RegisterMigration(t *testing.T) {
	reg := reconstruct.NewMigrationRegistry()

	err := reg.Register("user", 1, 2, func(ctx context.Context, data []byte) ([]byte, error) {
		var m map[string]any
		json.Unmarshal(data, &m)
		m["new_field"] = "default"
		return json.Marshal(m)
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
}

// SE-002: TestSchemaEvolution_GapRejected verifies that a non-consecutive migration
// (v1→v3) is rejected with ErrMigration.
func TestSchemaEvolution_GapRejected(t *testing.T) {
	reg := reconstruct.NewMigrationRegistry()

	err := reg.Register("user", 1, 3, func(ctx context.Context, data []byte) ([]byte, error) {
		return data, nil
	})
	if err == nil {
		t.Fatal("expected error for gap migration v1→v3")
	}
	if !errors.Is(err, domain.ErrMigration) {
		t.Fatalf("expected ErrMigration, got: %v", err)
	}
}

// SE-003: TestSchemaEvolution_DuplicateRejected verifies that registering the
// same fromVersion twice is rejected.
func TestSchemaEvolution_DuplicateRejected(t *testing.T) {
	reg := reconstruct.NewMigrationRegistry()

	noop := func(ctx context.Context, data []byte) ([]byte, error) { return data, nil }

	if err := reg.Register("user", 1, 2, noop); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	err := reg.Register("user", 1, 2, noop)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
	if !errors.Is(err, domain.ErrMigration) {
		t.Fatalf("expected ErrMigration, got: %v", err)
	}
}

// SE-004: TestSchemaEvolution_MigrateData applies a v1→v2 migration to data.
func TestSchemaEvolution_MigrateData(t *testing.T) {
	reg := reconstruct.NewMigrationRegistry()

	err := reg.Register("user", 1, 2, func(ctx context.Context, data []byte) ([]byte, error) {
		var m map[string]any
		json.Unmarshal(data, &m)
		m["Version2Field"] = "added"
		return json.Marshal(m)
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	original := []byte(`{"Name":"Alice"}`)
	migrated, ver, err := reg.Migrate(context.Background(), "user", original, 1, 2)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if ver != 2 {
		t.Fatalf("expected schema version 2, got %d", ver)
	}

	var m map[string]any
	json.Unmarshal(migrated, &m)
	if m["Version2Field"] != "added" {
		t.Fatalf("migration not applied: %v", m)
	}
}

// SE-005: TestSchemaEvolution_MigrationChain applies v1→v2→v3 in chain.
func TestSchemaEvolution_MigrationChain(t *testing.T) {
	reg := reconstruct.NewMigrationRegistry()

	reg.Register("user", 1, 2, func(ctx context.Context, data []byte) ([]byte, error) {
		var m map[string]any
		json.Unmarshal(data, &m)
		m["Field2"] = "v2"
		return json.Marshal(m)
	})

	reg.Register("user", 2, 3, func(ctx context.Context, data []byte) ([]byte, error) {
		var m map[string]any
		json.Unmarshal(data, &m)
		m["Field3"] = "v3"
		return json.Marshal(m)
	})

	original := []byte(`{"Name":"Alice"}`)
	migrated, ver, err := reg.Migrate(context.Background(), "user", original, 1, 3)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if ver != 3 {
		t.Fatalf("expected schema version 3, got %d", ver)
	}

	var m map[string]any
	json.Unmarshal(migrated, &m)
	if m["Field2"] != "v2" || m["Field3"] != "v3" {
		t.Fatalf("chain migration not applied: %v", m)
	}
}
