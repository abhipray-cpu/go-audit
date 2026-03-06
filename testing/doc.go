// Package audittest provides test utilities for go-audit consumers.
//
// This package includes:
//   - In-memory VersionWriterPort and VersionReaderPort for zero-infra testing
//   - NewAuditor(t) — a zero-config auditor constructor for unit tests
//   - Noop adapters for optional ports (WAL, cache, instrumenter, subscriber)
//   - Test assertion helpers (AssertVersionCount, AssertFieldChanged, etc.)
//   - Test fixtures and seed helpers
//   - TestInfra — testcontainers-based infrastructure for integration tests
//   - MockClock — deterministic time for reproducible tests
package audittest
