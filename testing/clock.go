package audittest

import (
	"sync"
	"time"

	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.ClockPort = (*MockClock)(nil)

// MockClock is a deterministic [port.ClockPort] for testing. It returns a
// fixed time that can be advanced programmatically.
//
// Usage:
//
//	clock := audittest.NewMockClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
//	auditor, _ := audit.New(audit.Config{
//	    Writer: store, Reader: store,
//	    Clock: clock,
//	})
//	clock.Advance(24 * time.Hour) // jump forward
type MockClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewMockClock creates a MockClock fixed at the given time.
func NewMockClock(t time.Time) *MockClock {
	return &MockClock{now: t}
}

// Now returns the current mock time.
func (c *MockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the mock clock forward by the given duration.
func (c *MockClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set sets the mock clock to a specific time.
func (c *MockClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
