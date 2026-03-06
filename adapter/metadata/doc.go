// Package metadata provides metadata extractor adapters for go-audit.
//
// Extractors pull audit metadata (actor, reason, correlation ID) from
// various sources: context.Context values, HTTP request headers, and
// gRPC metadata. The context extractor is the default.
package metadata
