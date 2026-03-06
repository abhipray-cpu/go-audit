package audit

import (
	"context"
	"strings"

	"github.com/abhipray-cpu/go-audit/domain"
)

// Context keys are unexported types to prevent collisions.
type ctxKey int

const (
	ctxKeyActorID ctxKey = iota
	ctxKeyActorType
	ctxKeyReason
	ctxKeyCorrelationID
	ctxKeyCustomMetadata
)

// sanitizeString strips null bytes (\x00) which are rejected by
// PostgreSQL text and JSON columns (SQLSTATE 22P05).
func sanitizeString(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

// WithActor attaches the actor's identity and type to the context.
// This metadata is automatically included in every version record created
// with this context.
//
//	ctx = audit.WithActor(ctx, "user-123", domain.ActorHuman)
func WithActor(ctx context.Context, id string, actorType domain.ActorType) context.Context {
	ctx = context.WithValue(ctx, ctxKeyActorID, sanitizeString(id))
	ctx = context.WithValue(ctx, ctxKeyActorType, actorType)
	return ctx
}

// WithReason attaches a human-readable reason to the context.
//
//	ctx = audit.WithReason(ctx, "quarterly address update")
func WithReason(ctx context.Context, reason string) context.Context {
	return context.WithValue(ctx, ctxKeyReason, sanitizeString(reason))
}

// WithCorrelationID attaches a correlation ID for cross-service tracing.
//
//	ctx = audit.WithCorrelationID(ctx, requestID)
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyCorrelationID, sanitizeString(id))
}

// WithCustomMetadata attaches an arbitrary key-value pair to the context.
// Multiple calls accumulate; later calls with the same key overwrite.
//
//	ctx = audit.WithCustomMetadata(ctx, "ticket", "JIRA-1234")
func WithCustomMetadata(ctx context.Context, key, value string) context.Context {
	existing := customMetadataFromCtx(ctx)
	// Copy to avoid mutating shared map.
	merged := make(map[string]string, len(existing)+1)
	for k, v := range existing {
		merged[k] = v
	}
	merged[sanitizeString(key)] = sanitizeString(value)
	return context.WithValue(ctx, ctxKeyCustomMetadata, merged)
}

// ---------- extractors (used by adapter/metadata and internally) ----------

// ActorFromCtx extracts the actor ID from the context, or "" if not set.
func ActorFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyActorID).(string)
	return v
}

// ActorTypeFromCtx extracts the actor type from the context.
func ActorTypeFromCtx(ctx context.Context) domain.ActorType {
	v, ok := ctx.Value(ctxKeyActorType).(domain.ActorType)
	if !ok {
		return domain.ActorHuman // zero-value default
	}
	return v
}

// ReasonFromCtx extracts the reason from the context, or "" if not set.
func ReasonFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyReason).(string)
	return v
}

// CorrelationIDFromCtx extracts the correlation ID, or "" if not set.
func CorrelationIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyCorrelationID).(string)
	return v
}

// customMetadataFromCtx extracts the custom metadata map, or nil if not set.
func customMetadataFromCtx(ctx context.Context) map[string]string {
	v, _ := ctx.Value(ctxKeyCustomMetadata).(map[string]string)
	return v
}

// MetadataFromCtx builds a complete [domain.VersionMetadata] from context values.
// This is the default extraction logic used by the context metadata adapter.
func MetadataFromCtx(ctx context.Context) domain.VersionMetadata {
	return domain.VersionMetadata{
		ActorID:       ActorFromCtx(ctx),
		ActorType:     ActorTypeFromCtx(ctx),
		Reason:        ReasonFromCtx(ctx),
		CorrelationID: CorrelationIDFromCtx(ctx),
		Custom:        customMetadataFromCtx(ctx),
	}
}
