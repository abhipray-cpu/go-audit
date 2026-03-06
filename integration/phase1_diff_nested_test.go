//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// ---------------------------------------------------------------------------
// Phase 1 — D-011 to D-018: Nested + deeply-nested struct diff tests
// ---------------------------------------------------------------------------

// D-011: TestDiff_Nested_LeafChange verifies a leaf change in a nested struct.
func TestDiff_Nested_LeafChange(t *testing.T) {
	e := diff.New()
	prev := &NestedEntity{
		ID:      "1",
		Name:    "Alice",
		Address: Address{Street: "1st St", City: "NYC", Country: "US", ZipCode: "10001"},
	}
	curr := &NestedEntity{
		ID:      "1",
		Name:    "Alice",
		Address: Address{Street: "1st St", City: "LA", Country: "US", ZipCode: "10001"},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Address.City" {
		t.Errorf("expected path Address.City, got %s", delta.Changes[0].Path)
	}
}

// D-012: TestDiff_Nested_MultipleLeafChanges verifies multiple leaf changes in same nest.
func TestDiff_Nested_MultipleLeafChanges(t *testing.T) {
	e := diff.New()
	prev := &NestedEntity{
		ID:      "1",
		Address: Address{Street: "1st St", City: "NYC", Country: "US", ZipCode: "10001"},
	}
	curr := &NestedEntity{
		ID:      "1",
		Address: Address{Street: "1st St", City: "LA", Country: "US", ZipCode: "90001"},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	paths := map[string]bool{}
	for _, c := range delta.Changes {
		paths[c.Path] = true
	}
	if !paths["Address.City"] || !paths["Address.ZipCode"] {
		t.Errorf("expected Address.City and Address.ZipCode, got %v", paths)
	}
}

// D-013: TestDiff_Nested_EntireNestedChange verifies all nested fields changing.
func TestDiff_Nested_EntireNestedChange(t *testing.T) {
	e := diff.New()
	prev := &NestedEntity{
		ID:      "1",
		Address: Address{Street: "1st St", City: "NYC", Country: "US", ZipCode: "10001"},
	}
	curr := &NestedEntity{
		ID:      "1",
		Address: Address{Street: "2nd Ave", City: "LA", Country: "UK", ZipCode: "SW1"},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 4 {
		t.Fatalf("expected 4 changes, got %d: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-014: TestDiff_Nested_CrossNestChange verifies changes across different nested structs.
func TestDiff_Nested_CrossNestChange(t *testing.T) {
	e := diff.New()
	prev := &NestedEntity{
		ID:      "1",
		Address: Address{City: "NYC"},
		Contact: Contact{Email: "old@test.com"},
	}
	curr := &NestedEntity{
		ID:      "1",
		Address: Address{City: "LA"},
		Contact: Contact{Email: "new@test.com"},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	paths := map[string]bool{}
	for _, c := range delta.Changes {
		paths[c.Path] = true
	}
	if !paths["Address.City"] || !paths["Contact.Email"] {
		t.Errorf("expected Address.City and Contact.Email, got %v", paths)
	}
}

// D-015: TestDiff_Nested_NoNestedChange verifies only top-level field change detected.
func TestDiff_Nested_NoNestedChange(t *testing.T) {
	e := diff.New()
	prev := &NestedEntity{
		ID:      "1",
		Name:    "Alice",
		Address: Address{City: "NYC"},
	}
	curr := &NestedEntity{
		ID:      "1",
		Name:    "Bob",
		Address: Address{City: "NYC"},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Name" {
		t.Errorf("expected path Name, got %s", delta.Changes[0].Path)
	}
}

// D-016: TestDiff_DeepNest_4Level verifies change detection at depth 4.
func TestDiff_DeepNest_4Level(t *testing.T) {
	e := diff.New()
	prev := &Company{
		ID:   "1",
		Name: "Acme",
		Headquarters: Office{
			Name: "HQ",
			Location: Location{
				Label: "Main",
				Coord: GeoCoord{Lat: 40.7, Lng: -74.0},
			},
		},
	}
	curr := &Company{
		ID:   "1",
		Name: "Acme",
		Headquarters: Office{
			Name: "HQ",
			Location: Location{
				Label: "Main",
				Coord: GeoCoord{Lat: 34.0, Lng: -74.0},
			},
		},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Headquarters.Location.Coord.Lat" {
		t.Errorf("expected path Headquarters.Location.Coord.Lat, got %s", delta.Changes[0].Path)
	}
}

// D-017: TestDiff_DeepNest_MultiLevel verifies changes at different depths simultaneously.
func TestDiff_DeepNest_MultiLevel(t *testing.T) {
	e := diff.New()
	prev := &Company{
		ID:   "1",
		Name: "Acme",
		Headquarters: Office{
			Name: "HQ",
			Location: Location{
				Label: "Main",
				Coord: GeoCoord{Lat: 40.7, Lng: -74.0},
			},
		},
	}
	curr := &Company{
		ID:   "1",
		Name: "Acme Inc.",
		Headquarters: Office{
			Name: "HQ",
			Location: Location{
				Label: "Main",
				Coord: GeoCoord{Lat: 40.7, Lng: -118.2},
			},
		},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-018: TestDiff_DeepNest_IntermediateLevel verifies change at an intermediate level.
func TestDiff_DeepNest_IntermediateLevel(t *testing.T) {
	e := diff.New()
	prev := &Company{
		ID: "1",
		Headquarters: Office{
			Location: Location{Label: "Main", Coord: GeoCoord{Lat: 40.7, Lng: -74.0}},
		},
	}
	curr := &Company{
		ID: "1",
		Headquarters: Office{
			Location: Location{Label: "Secondary", Coord: GeoCoord{Lat: 40.7, Lng: -74.0}},
		},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Headquarters.Location.Label" {
		t.Errorf("expected Headquarters.Location.Label, got %s", delta.Changes[0].Path)
	}
}
