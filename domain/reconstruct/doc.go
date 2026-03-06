// Package reconstruct implements entity state reconstruction from delta chains,
// compaction, and schema evolution for go-audit.
//
// When using Delta or Hybrid storage strategies, this package rebuilds the full
// entity state by applying a chain of deltas to the nearest snapshot. It also
// handles background compaction to bound delta chain length, and lazy on-read
// schema migration.
package reconstruct
