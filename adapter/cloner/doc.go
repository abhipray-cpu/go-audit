// Package cloner provides the default reflection-based deep cloner for go-audit.
//
// This package implements the ClonerPort using reflection to create independent
// deep copies of entities before they enter the async pipeline. For hot-path
// performance, a code generation tool is available (see cmd/go-audit-gen).
package cloner
