// Package port defines the hexagonal architecture port interfaces for go-audit.
//
// Ports are the contracts between the domain and the outside world. They are split
// into two categories:
//
//   - Outbound ports (15): interfaces that the domain requires adapters to implement
//     (e.g., VersionWriterPort, CachePort, WALPort).
//   - Inbound ports (3): interfaces that the domain exposes to callers
//     (e.g., VersioningPort, QueryPort, SchemaPort).
//
// This package imports nothing outside of the domain package.
package port
