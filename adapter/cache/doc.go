// Package cache provides the LRU cache adapter for go-audit.
//
// This package implements the CachePort with an in-process LRU cache
// for frequently accessed version records. Write-through semantics
// ensure the cache is always up-to-date.
package cache
