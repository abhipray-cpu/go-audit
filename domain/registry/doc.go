// Package registry manages entity type registration and field annotation parsing
// for go-audit.
//
// Before an entity can be versioned, it must be registered. The registry inspects
// the struct via reflection, parses version:"..." tags (id, tracked, ignore,
// normalized, redactable), and stores the configuration for use by the diff engine
// and serializer.
//
// The registry also enforces guardrails: rejecting entities without an ID field,
// warning on []byte fields, and rejecting oversized entities.
package registry
