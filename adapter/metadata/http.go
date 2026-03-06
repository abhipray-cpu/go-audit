package metadata

import (
	"context"
	"net/http"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time check.
var _ port.MetadataExtractorPort = (*HTTPExtractor)(nil)

// httpRequestKey is the context key used to stash an *http.Request.
type httpRequestKey struct{}

// WithHTTPRequest stores an HTTP request in the context so that
// [HTTPExtractor] can pull headers from it later.
//
//	ctx = metadata.WithHTTPRequest(ctx, r)
func WithHTTPRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, httpRequestKey{}, r)
}

// HTTPExtractor implements [port.MetadataExtractorPort] by reading
// well-known HTTP headers from the request stored in the context.
//
// Recognised headers:
//
//	X-Actor-ID        → ActorID
//	X-Actor-Type      → ActorType (human | service | system)
//	X-Audit-Reason    → Reason
//	X-Correlation-ID  → CorrelationID
//	X-Request-ID      → CorrelationID (fallback)
//	Traceparent       → TraceID (first 32 hex chars)
type HTTPExtractor struct{}

// NewHTTPExtractor creates an [HTTPExtractor].
func NewHTTPExtractor() *HTTPExtractor { return &HTTPExtractor{} }

// Extract reads audit metadata from HTTP headers.
func (e *HTTPExtractor) Extract(ctx context.Context) domain.VersionMetadata {
	r, _ := ctx.Value(httpRequestKey{}).(*http.Request)
	if r == nil {
		return domain.VersionMetadata{}
	}

	meta := domain.VersionMetadata{
		ActorID: r.Header.Get("X-Actor-ID"),
		Reason:  r.Header.Get("X-Audit-Reason"),
		Source:  "http",
	}

	switch r.Header.Get("X-Actor-Type") {
	case "human":
		meta.ActorType = domain.ActorHuman
	case "service":
		meta.ActorType = domain.ActorService
	case "system":
		meta.ActorType = domain.ActorSystem
	}

	if cid := r.Header.Get("X-Correlation-ID"); cid != "" {
		meta.CorrelationID = cid
	} else if rid := r.Header.Get("X-Request-ID"); rid != "" {
		meta.CorrelationID = rid
	}

	if tp := r.Header.Get("Traceparent"); len(tp) >= 55 {
		// W3C traceparent: version-traceID-spanID-flags
		// 00-<32 hex traceID>-<16 hex spanID>-<2 hex flags>
		meta.TraceID = tp[3:35]
		meta.SpanID = tp[36:52]
	}

	return meta
}
