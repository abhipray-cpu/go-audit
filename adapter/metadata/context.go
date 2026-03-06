package metadata

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.MetadataExtractorPort = (*ContextExtractor)(nil)

// ExtractFunc is a function that extracts metadata from a context.
// This allows injection of the audit package's MetadataFromCtx without
// creating a circular import.
type ExtractFunc func(ctx context.Context) domain.VersionMetadata

// ContextExtractor implements [port.MetadataExtractorPort] by delegating
// to an injected extraction function. The default extraction returns
// zero-value metadata (no panic on empty context).
//
// Usage:
//
//	extractor := metadata.NewContextExtractor(audit.MetadataFromCtx)
type ContextExtractor struct {
	fn ExtractFunc
}

// NewContextExtractor returns a ContextExtractor that delegates to fn.
// If fn is nil, Extract always returns zero-value metadata.
func NewContextExtractor(fn ExtractFunc) *ContextExtractor {
	return &ContextExtractor{fn: fn}
}

// Extract pulls audit metadata from the context.
func (e *ContextExtractor) Extract(ctx context.Context) domain.VersionMetadata {
	if e.fn != nil {
		return e.fn(ctx)
	}
	return domain.VersionMetadata{}
}
