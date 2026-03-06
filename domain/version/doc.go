// Package version implements version sequencing, strategy selection, and the
// core versioning pipeline for go-audit.
//
// This package is responsible for:
//   - Monotonic version number generation per entity
//   - Strategy selection (Full, Delta, Hybrid)
//   - Orchestrating the clone → diff → serialize → write pipeline
package version
