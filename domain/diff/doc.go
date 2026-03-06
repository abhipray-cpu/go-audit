// Package diff implements the structural diff engine for go-audit.
//
// The diff engine compares two versions of an entity and produces a [domain.Delta]
// describing which fields changed, with old and new values. It supports 10 data shapes:
// flat structs, nested structs, maps, slices, pointers, embedded structs,
// interface{}/any, nested JSON, time.Time, and custom types.
//
// The engine uses reflection with graceful degradation — unsupported types
// trigger a fallback to full-snapshot mode rather than panicking.
package diff
