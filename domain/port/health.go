package port

import (
	"context"
)

// HealthCheckPort reports the health of all configured storage backends.
type HealthCheckPort interface {
	// Check returns the health status of each backend.
	Check(ctx context.Context) []HealthStatus
}
