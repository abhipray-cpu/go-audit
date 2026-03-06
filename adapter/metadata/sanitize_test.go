package metadata

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
)

// stubExtractor returns fixed metadata for testing.
type stubExtractor struct {
	meta domain.VersionMetadata
}

func (s *stubExtractor) Extract(_ context.Context) domain.VersionMetadata {
	return s.meta
}

func TestMetadata_NoCredentials(t *testing.T) {
	inner := &stubExtractor{
		meta: domain.VersionMetadata{
			ActorID: "user-1",
			Custom: map[string]string{
				"ticket":       "JIRA-123",
				"password":     "s3cret",
				"api_token":    "tok-abc",
				"db_secret":    "hidden",
				"normal_key":   "visible",
				"API_KEY":      "should-be-redacted",
				"x-auth-token": "bearer-xyz",
			},
		},
	}

	sanitizer := NewSanitizingExtractor(inner)
	meta := sanitizer.Extract(context.Background())

	// Non-sensitive keys preserved.
	if meta.Custom["ticket"] != "JIRA-123" {
		t.Errorf("ticket = %q, want JIRA-123", meta.Custom["ticket"])
	}
	if meta.Custom["normal_key"] != "visible" {
		t.Errorf("normal_key = %q, want visible", meta.Custom["normal_key"])
	}

	// ActorID preserved (not in custom map).
	if meta.ActorID != "user-1" {
		t.Errorf("ActorID = %q, want user-1", meta.ActorID)
	}

	// Sensitive keys redacted.
	sensitiveChecks := map[string]string{
		"password":     "[REDACTED]",
		"api_token":    "[REDACTED]",
		"db_secret":    "[REDACTED]",
		"API_KEY":      "[REDACTED]",
		"x-auth-token": "[REDACTED]",
	}
	for k, want := range sensitiveChecks {
		if meta.Custom[k] != want {
			t.Errorf("Custom[%q] = %q, want %q", k, meta.Custom[k], want)
		}
	}
}

func TestMetadata_NilCustom(t *testing.T) {
	inner := &stubExtractor{
		meta: domain.VersionMetadata{ActorID: "u1"},
	}
	sanitizer := NewSanitizingExtractor(inner)
	meta := sanitizer.Extract(context.Background())

	if meta.Custom != nil {
		t.Errorf("expected nil Custom, got %v", meta.Custom)
	}
}

func TestMetadata_EmptyCustom(t *testing.T) {
	inner := &stubExtractor{
		meta: domain.VersionMetadata{Custom: map[string]string{}},
	}
	sanitizer := NewSanitizingExtractor(inner)
	meta := sanitizer.Extract(context.Background())

	if len(meta.Custom) != 0 {
		t.Errorf("expected empty Custom map, got %v", meta.Custom)
	}
}
