package health

import (
	"context"
	"time"

	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time check.
var _ port.HealthCheckPort = (*Checker)(nil)

// Probe is a function that checks the health of a single backend.
// It returns an error if the backend is unhealthy.
type Probe struct {
	// Name identifies the backend (e.g., "postgres", "clickhouse").
	Name string

	// Check pings the backend. Returns nil on success.
	Check func(ctx context.Context) error
}

// Checker implements [port.HealthCheckPort] by running a list of
// configurable probes and returning their aggregate status.
type Checker struct {
	probes []Probe
}

// New creates a [Checker] with the given probes.
func New(probes ...Probe) *Checker {
	return &Checker{probes: probes}
}

// Check runs all probes and returns a [port.HealthStatus] per backend.
func (c *Checker) Check(ctx context.Context) []port.HealthStatus {
	results := make([]port.HealthStatus, len(c.probes))

	for i, p := range c.probes {
		start := time.Now()
		err := p.Check(ctx)
		latency := time.Since(start)

		hs := port.HealthStatus{
			Name:    p.Name,
			Healthy: err == nil,
			Latency: latency,
		}
		if err != nil {
			hs.Error = err.Error()
		}
		results[i] = hs
	}

	return results
}
