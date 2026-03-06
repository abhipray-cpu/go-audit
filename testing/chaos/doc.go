// Package chaos contains chaos engineering test scenarios for go-audit.
//
// These tests simulate infrastructure failures (process crashes, network
// partitions, disk full, OOM) using toxiproxy and testcontainers to verify
// the library's resilience and recovery behavior.
//
// Run with: go test -tags chaos -timeout 20m ./testing/chaos/...
package chaos
