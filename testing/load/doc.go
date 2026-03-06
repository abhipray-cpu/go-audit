// Package load contains load test scenarios for go-audit.
//
// These tests exercise the library under sustained high throughput,
// burst traffic, and mixed read/write workloads. They require Docker
// for backend containers.
//
// Run with: go test -tags loadtest -timeout 45m ./testing/load/...
package load
