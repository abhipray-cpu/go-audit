// Package hooks implements authorization and lifecycle hooks for go-audit.
//
// Hooks allow callers to inject custom logic before reads and writes. Common
// use cases include authorization checks, tenant isolation, and custom validation.
// Hooks are executed in registration order with panic recovery.
package hooks
