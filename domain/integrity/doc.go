// Package integrity implements hash chain verification for go-audit.
//
// When enabled, each version record includes a SHA-256 hash that chains to the
// previous version's hash, creating a tamper-evident audit trail. This package
// provides the computation and verification logic.
package integrity
