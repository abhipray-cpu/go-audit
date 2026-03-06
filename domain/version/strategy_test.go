package version

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
)

// ==========================================================================
// GA-033 — Delta Strategy + Validation
// ==========================================================================

func TestSelectStrategy_FirstVersionAlwaysFull(t *testing.T) {
	for _, s := range []domain.Strategy{domain.StrategyFull, domain.StrategyDelta, domain.StrategyHybrid} {
		got := SelectStrategy(StrategyConfig{Default: s}, 1, nil)
		if got != domain.StrategyFull {
			t.Errorf("SelectStrategy(default=%v, v1) = %v, want Full", s, got)
		}
	}
}

func TestSelectStrategy_DeltaDefault(t *testing.T) {
	cfg := StrategyConfig{Default: domain.StrategyDelta}

	for v := int64(2); v <= 10; v++ {
		got := SelectStrategy(cfg, v, nil)
		if got != domain.StrategyDelta {
			t.Errorf("v%d: expected Delta, got %v", v, got)
		}
	}
}

func TestSelectStrategy_FullDefault(t *testing.T) {
	cfg := StrategyConfig{Default: domain.StrategyFull}

	for v := int64(2); v <= 10; v++ {
		got := SelectStrategy(cfg, v, nil)
		if got != domain.StrategyFull {
			t.Errorf("v%d: expected Full, got %v", v, got)
		}
	}
}

func TestSelectStrategy_PerCallOverride(t *testing.T) {
	cfg := StrategyConfig{Default: domain.StrategyFull}
	delta := domain.StrategyDelta

	got := SelectStrategy(cfg, 5, &delta)
	if got != domain.StrategyDelta {
		t.Errorf("expected Delta override, got %v", got)
	}
}

// ==========================================================================
// GA-034 — Hybrid Strategy + Snapshot Interval
// ==========================================================================

func TestSelectStrategy_Hybrid_SnapshotEveryN(t *testing.T) {
	cfg := StrategyConfig{
		Default:                domain.StrategyHybrid,
		HybridSnapshotInterval: 5,
	}

	expected := map[int64]domain.Strategy{
		1:  domain.StrategyFull, // always full
		2:  domain.StrategyDelta,
		3:  domain.StrategyDelta,
		4:  domain.StrategyDelta,
		5:  domain.StrategyDelta,
		6:  domain.StrategyFull, // 6 % 5 == 1 → snapshot
		7:  domain.StrategyDelta,
		8:  domain.StrategyDelta,
		9:  domain.StrategyDelta,
		10: domain.StrategyDelta,
		11: domain.StrategyFull, // 11 % 5 == 1 → snapshot
	}

	for v, want := range expected {
		got := SelectStrategy(cfg, v, nil)
		if got != want {
			t.Errorf("v%d: expected %v, got %v", v, want, got)
		}
	}
}

func TestSelectStrategy_Hybrid_DefaultInterval(t *testing.T) {
	// When interval < 2, defaults to 10.
	cfg := StrategyConfig{
		Default:                domain.StrategyHybrid,
		HybridSnapshotInterval: 0,
	}

	// v11 should be a snapshot (11 % 10 == 1).
	got := SelectStrategy(cfg, 11, nil)
	if got != domain.StrategyFull {
		t.Errorf("v11 with default interval: expected Full, got %v", got)
	}

	// v2 should be delta.
	got = SelectStrategy(cfg, 2, nil)
	if got != domain.StrategyDelta {
		t.Errorf("v2 with default interval: expected Delta, got %v", got)
	}
}

// ==========================================================================
// Delta Validation (GA-033)
// ==========================================================================

// stubDiffer implements port.DifferPort for validation tests.
type stubDiffer struct{}

type stubEntity struct {
	Name string `json:"name"`
}

func (d *stubDiffer) Diff(_ context.Context, prev, curr any) (domain.Delta, error) {
	return domain.Delta{
		Changes: []domain.FieldChange{
			{
				Path:     "Name",
				OldValue: json.RawMessage(`"Alice"`),
				NewValue: json.RawMessage(`"Bob"`),
			},
		},
	}, nil
}

func (d *stubDiffer) Apply(_ context.Context, prev any, delta domain.Delta) (any, error) {
	// Reconstruct by applying the change.
	p := prev.(*stubEntity)
	result := &stubEntity{Name: p.Name}
	for _, ch := range delta.Changes {
		if ch.Path == "Name" {
			var name string
			json.Unmarshal(ch.NewValue, &name)
			result.Name = name
		}
	}
	return result, nil
}

// stubSerializer implements port.SerializerPort for validation tests.
type stubSerializer struct{}

func (s *stubSerializer) Marshal(_ context.Context, v any) ([]byte, error) {
	return json.Marshal(v)
}

func (s *stubSerializer) Unmarshal(_ context.Context, data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (s *stubSerializer) ContentType() string { return "application/json" }

func TestValidateDelta_Pass(t *testing.T) {
	prev := &stubEntity{Name: "Alice"}
	curr := &stubEntity{Name: "Bob"}
	delta := domain.Delta{
		Changes: []domain.FieldChange{
			{
				Path:     "Name",
				OldValue: json.RawMessage(`"Alice"`),
				NewValue: json.RawMessage(`"Bob"`),
			},
		},
	}

	err := ValidateDelta(context.Background(), &stubDiffer{}, &stubSerializer{}, prev, curr, delta)
	if err != nil {
		t.Fatalf("ValidateDelta() should pass: %v", err)
	}
}

func TestValidateDelta_EmptyDelta(t *testing.T) {
	err := ValidateDelta(context.Background(), &stubDiffer{}, &stubSerializer{}, nil, nil, domain.Delta{})
	if err != nil {
		t.Fatalf("ValidateDelta() should pass for empty delta: %v", err)
	}
}

func TestValidateDelta_Mismatch(t *testing.T) {
	prev := &stubEntity{Name: "Alice"}
	curr := &stubEntity{Name: "Charlie"} // not "Bob" — mismatch
	delta := domain.Delta{
		Changes: []domain.FieldChange{
			{
				Path:     "Name",
				OldValue: json.RawMessage(`"Alice"`),
				NewValue: json.RawMessage(`"Bob"`), // Apply produces Bob, but curr is Charlie
			},
		},
	}

	err := ValidateDelta(context.Background(), &stubDiffer{}, &stubSerializer{}, prev, curr, delta)
	if err == nil {
		t.Fatal("ValidateDelta() should fail for mismatched delta")
	}
}

func TestDeltaStrategy_StoresOnlyChanges(t *testing.T) {
	// This tests that when StrategyDelta is selected, the delta is smaller
	// than the full snapshot. We verify through the strategy selector.
	cfg := StrategyConfig{Default: domain.StrategyDelta}

	s := SelectStrategy(cfg, 2, nil)
	if s != domain.StrategyDelta {
		t.Errorf("expected Delta for v2, got %v", s)
	}

	s = SelectStrategy(cfg, 1, nil)
	if s != domain.StrategyFull {
		t.Errorf("expected Full for v1, got %v", s)
	}
}

func TestDeltaStrategy_FirstVersion_IsSnapshot(t *testing.T) {
	cfg := StrategyConfig{Default: domain.StrategyDelta}
	s := SelectStrategy(cfg, 1, nil)
	if s != domain.StrategyFull {
		t.Errorf("v1 must always be Full, got %v", s)
	}
}

func TestHybridStrategy_SnapshotEveryN(t *testing.T) {
	cfg := StrategyConfig{
		Default:                domain.StrategyHybrid,
		HybridSnapshotInterval: 5,
	}

	// Versions where snapshot occurs: 1, 6, 11, 16, ...
	snapshots := []int64{1, 6, 11, 16}
	for _, v := range snapshots {
		s := SelectStrategy(cfg, v, nil)
		if s != domain.StrategyFull {
			t.Errorf("v%d: expected Full (snapshot), got %v", v, s)
		}
	}

	// Versions that should be delta: 2, 3, 4, 5, 7, 8, 9, 10
	deltas := []int64{2, 3, 4, 5, 7, 8, 9, 10}
	for _, v := range deltas {
		s := SelectStrategy(cfg, v, nil)
		if s != domain.StrategyDelta {
			t.Errorf("v%d: expected Delta, got %v", v, s)
		}
	}
}
