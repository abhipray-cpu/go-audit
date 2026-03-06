package version

import (
	"context"
	"fmt"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// StrategyConfig holds the configuration for strategy selection.
type StrategyConfig struct {
	// Default is the default storage strategy (Full, Delta, or Hybrid).
	Default domain.Strategy

	// HybridSnapshotInterval controls how often a full snapshot is stored
	// when using StrategyHybrid. Every N-th version is a full snapshot;
	// all others are deltas. Must be ≥ 2. Default: 10.
	HybridSnapshotInterval int
}

// SelectStrategy determines the effective storage strategy for a version based
// on the per-call override, the default config, and the version number.
//
// Rules:
//   - Version 1 is ALWAYS StrategyFull (there is no previous to diff against).
//   - If an explicit per-call strategy is provided, it takes priority over the default.
//   - For StrategyHybrid: every N-th version (where N = HybridSnapshotInterval) is
//     StrategyFull; all others are StrategyDelta.
//   - For StrategyDelta: always delta (except v1).
//   - For StrategyFull: always full snapshot.
func SelectStrategy(cfg StrategyConfig, nextVersion int64, perCallOverride *domain.Strategy) domain.Strategy {
	// Version 1 is always a full snapshot — no previous to diff against.
	if nextVersion <= 1 {
		return domain.StrategyFull
	}

	// Per-call override takes priority.
	strategy := cfg.Default
	if perCallOverride != nil {
		strategy = *perCallOverride
	}

	switch strategy {
	case domain.StrategyHybrid:
		interval := cfg.HybridSnapshotInterval
		if interval < 2 {
			interval = 10
		}
		// Every N-th version is a full snapshot.
		if nextVersion%int64(interval) == 1 {
			return domain.StrategyFull
		}
		return domain.StrategyDelta

	case domain.StrategyDelta:
		return domain.StrategyDelta

	default:
		return domain.StrategyFull
	}
}

// ValidateDelta verifies that applying a delta to the previous entity state
// produces the expected current entity state. This is called after computing a
// delta to catch diff/apply bugs early.
//
// Returns nil if validation passes, or an error describing the mismatch.
func ValidateDelta(
	ctx context.Context,
	differ port.DifferPort,
	serializer port.SerializerPort,
	prev any,
	current any,
	delta domain.Delta,
) error {
	if delta.IsEmpty() {
		return nil // no changes to validate
	}

	// Apply delta to prev → reconstructed.
	reconstructed, err := differ.Apply(ctx, prev, delta)
	if err != nil {
		return fmt.Errorf("%w: delta validation apply failed: %v", domain.ErrDiff, err)
	}

	// Serialize both and compare bytes.
	currentBytes, err := serializer.Marshal(ctx, current)
	if err != nil {
		return fmt.Errorf("%w: delta validation marshal current: %v", domain.ErrDiff, err)
	}

	reconBytes, err := serializer.Marshal(ctx, reconstructed)
	if err != nil {
		return fmt.Errorf("%w: delta validation marshal reconstructed: %v", domain.ErrDiff, err)
	}

	if string(currentBytes) != string(reconBytes) {
		return fmt.Errorf(
			"%w: delta validation failed: Apply(prev, delta) != current",
			domain.ErrDiff,
		)
	}

	return nil
}
