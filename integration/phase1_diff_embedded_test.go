//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// ---------------------------------------------------------------------------
// Phase 1 — D-054 to D-056: Embedded (promoted) struct diff tests
// ---------------------------------------------------------------------------

// EmbTestAuditFields is an embedded struct without version tags,
// used to test promoted field behavior without the "tracked" filter
// interfering.
type EmbTestAuditFields struct {
	CreatedBy string
	UpdatedBy string
}

// EmbTestEntity has an anonymous struct with no tracked tags,
// ensuring promoted field traversal is exercised.
type EmbTestEntity struct {
	ID   string `version:"id"`
	Name string
	EmbTestAuditFields
}

// D-054: TestDiff_Embedded_PromotedFieldChange verifies promoted field paths have no prefix.
func TestDiff_Embedded_PromotedFieldChange(t *testing.T) {
	e := diff.New()
	prev := &EmbTestEntity{ID: "1", Name: "Alice", EmbTestAuditFields: EmbTestAuditFields{CreatedBy: "a"}}
	curr := &EmbTestEntity{ID: "1", Name: "Alice", EmbTestAuditFields: EmbTestAuditFields{CreatedBy: "b"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	// Promoted fields appear as top-level paths, not "EmbTestAuditFields.CreatedBy".
	if delta.Changes[0].Path != "CreatedBy" {
		t.Errorf("expected promoted path CreatedBy, got %s", delta.Changes[0].Path)
	}
}

// D-055: TestDiff_Embedded_MixedChange verifies changes in both embedded and normal fields.
func TestDiff_Embedded_MixedChange(t *testing.T) {
	e := diff.New()
	prev := &EmbTestEntity{
		ID:                 "1",
		Name:               "Alice",
		EmbTestAuditFields: EmbTestAuditFields{CreatedBy: "a", UpdatedBy: "a"},
	}
	curr := &EmbTestEntity{
		ID:                 "1",
		Name:               "Bob",
		EmbTestAuditFields: EmbTestAuditFields{CreatedBy: "a", UpdatedBy: "b"},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-056: TestDiff_Embedded_NoChange verifies no false positives for identical embedded fields.
func TestDiff_Embedded_NoChange(t *testing.T) {
	e := diff.New()
	prev := &EmbTestEntity{
		ID:                 "1",
		Name:               "Alice",
		EmbTestAuditFields: EmbTestAuditFields{CreatedBy: "a", UpdatedBy: "b"},
	}
	curr := &EmbTestEntity{
		ID:                 "1",
		Name:               "Alice",
		EmbTestAuditFields: EmbTestAuditFields{CreatedBy: "a", UpdatedBy: "b"},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta, got %d changes", len(delta.Changes))
	}
}

// D-054b: TestDiff_Embedded_TrackedSkipsUntaggedEmbedded documents the behavior where
// embedded structs without "tracked" tag are skipped when any field has "tracked".
// This is a known limitation/design decision of the diff engine.
func TestDiff_Embedded_TrackedSkipsUntaggedEmbedded(t *testing.T) {
	e := diff.New()
	// Using the original EmbeddedEntity which has Name `version:"tracked"` but
	// AuditFields has no version tags.
	prev := &EmbeddedEntity{ID: "1", Name: "Alice", AuditFields: AuditFields{CreatedBy: "a"}}
	curr := &EmbeddedEntity{ID: "1", Name: "Alice", AuditFields: AuditFields{CreatedBy: "b"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With tracked filtering, the embedded AuditFields (not tagged tracked)
	// is skipped entirely — 0 changes detected even though CreatedBy changed.
	if len(delta.Changes) != 0 {
		t.Fatalf("expected 0 changes (tracked filter skips untagged embedded), got %d: %+v",
			len(delta.Changes), delta.Changes)
	}
}
