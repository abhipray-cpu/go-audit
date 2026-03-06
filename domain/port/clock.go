package port

import "time"

// ClockPort provides the current time. Swap in a mock implementation
// for deterministic time in tests.
type ClockPort interface {
	// Now returns the current time.
	Now() time.Time
}
