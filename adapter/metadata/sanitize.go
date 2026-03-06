package metadata

import (
	"context"
	"strings"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.MetadataExtractorPort = (*SanitizingExtractor)(nil)

// sensitiveKeys is the default set of metadata key substrings that trigger
// redaction. Any custom metadata key containing one of these substrings
// (case-insensitive) will have its value stripped.
var sensitiveKeys = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"api_key",
	"apikey",
	"credential",
	"private_key",
	"privatekey",
	"auth",
}

// SanitizingExtractor wraps another [port.MetadataExtractorPort] and strips
// credentials from the extracted metadata. Any custom metadata key whose
// name contains a sensitive substring (password, token, secret, etc.) has
// its value replaced with "[REDACTED]".
type SanitizingExtractor struct {
	inner port.MetadataExtractorPort
}

// NewSanitizingExtractor wraps inner with credential stripping.
func NewSanitizingExtractor(inner port.MetadataExtractorPort) *SanitizingExtractor {
	return &SanitizingExtractor{inner: inner}
}

// Extract delegates to the inner extractor and then sanitizes the result.
func (s *SanitizingExtractor) Extract(ctx context.Context) domain.VersionMetadata {
	meta := s.inner.Extract(ctx)
	meta.Custom = sanitizeMap(meta.Custom)
	return meta
}

// sanitizeMap returns a copy of m with sensitive keys redacted.
func sanitizeMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cleaned := make(map[string]string, len(m))
	for k, v := range m {
		if isSensitiveKey(k) {
			cleaned[k] = "[REDACTED]"
		} else {
			cleaned[k] = v
		}
	}
	return cleaned
}

// isSensitiveKey returns true if key contains any sensitive substring.
func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}
