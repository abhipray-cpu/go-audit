package subscriber

import (
	"context"
	"fmt"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time check.
var _ port.SubscriberPort = (*Log)(nil)

// Log is a subscriber that logs every version event using the provided logger.
// It serves as the default subscriber when no others are configured.
type Log struct {
	logger port.LoggerPort
}

// NewLog creates a [Log] subscriber.
func NewLog(logger port.LoggerPort) *Log {
	return &Log{logger: logger}
}

// OnVersion logs the version creation event.
func (l *Log) OnVersion(_ context.Context, record domain.VersionRecord) error {
	l.logger.Info("audit: version created",
		"entity_type", record.EntityType,
		"entity_id", record.EntityID,
		"version", record.Version,
		"actor_id", record.Metadata.ActorID,
		"strategy", fmt.Sprintf("%d", record.Strategy),
	)
	return nil
}
