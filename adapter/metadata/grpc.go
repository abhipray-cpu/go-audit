package metadata

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time check.
var _ port.MetadataExtractorPort = (*GRPCExtractor)(nil)

// grpcMetadataKey is the context key for gRPC-style metadata.
type grpcMetadataKey struct{}

// GRPCMetadata is a simple string-to-string-slice map, compatible with
// google.golang.org/grpc/metadata.MD without importing gRPC.
//
// In production, convert from grpc/metadata.MD:
//
//	md, _ := metadata.FromIncomingContext(ctx)
//	ctx = metadata.WithGRPCMetadata(ctx, metadata.GRPCMetadata(md))
type GRPCMetadata map[string][]string

// Get returns the first value for key, or empty string.
func (m GRPCMetadata) Get(key string) string {
	if vals := m[key]; len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// WithGRPCMetadata stores gRPC metadata in the context for extraction by
// [GRPCExtractor].
func WithGRPCMetadata(ctx context.Context, md GRPCMetadata) context.Context {
	return context.WithValue(ctx, grpcMetadataKey{}, md)
}

// GRPCExtractor implements [port.MetadataExtractorPort] by reading
// well-known gRPC metadata keys.
//
// Recognised keys:
//
//	x-actor-id        → ActorID
//	x-actor-type      → ActorType
//	x-audit-reason    → Reason
//	x-correlation-id  → CorrelationID
//	x-request-id      → CorrelationID (fallback)
//	x-trace-id        → TraceID
//	x-span-id         → SpanID
type GRPCExtractor struct{}

// NewGRPCExtractor creates a [GRPCExtractor].
func NewGRPCExtractor() *GRPCExtractor { return &GRPCExtractor{} }

// Extract reads audit metadata from gRPC metadata stored in the context.
func (e *GRPCExtractor) Extract(ctx context.Context) domain.VersionMetadata {
	md, _ := ctx.Value(grpcMetadataKey{}).(GRPCMetadata)
	if md == nil {
		return domain.VersionMetadata{}
	}

	meta := domain.VersionMetadata{
		ActorID: md.Get("x-actor-id"),
		Reason:  md.Get("x-audit-reason"),
		TraceID: md.Get("x-trace-id"),
		SpanID:  md.Get("x-span-id"),
		Source:  "grpc",
	}

	switch md.Get("x-actor-type") {
	case "human":
		meta.ActorType = domain.ActorHuman
	case "service":
		meta.ActorType = domain.ActorService
	case "system":
		meta.ActorType = domain.ActorSystem
	}

	if cid := md.Get("x-correlation-id"); cid != "" {
		meta.CorrelationID = cid
	} else if rid := md.Get("x-request-id"); rid != "" {
		meta.CorrelationID = rid
	}

	return meta
}
