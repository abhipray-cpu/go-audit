// Package subscriber provides SubscriberPort implementations for go-audit.
//
// Fanout dispatches version events to multiple subscribers concurrently
// with per-subscriber failure isolation and configurable timeouts.
// LogSubscriber is a simple subscriber that logs every version event.
package subscriber

import (
	"context"
	"sync"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time check.
var _ port.SubscriberPort = (*Fanout)(nil)

// DefaultTimeout is the default per-subscriber timeout.
const DefaultTimeout = 5 * time.Second

// Fanout dispatches version events to multiple subscribers concurrently.
// Each subscriber runs in its own goroutine with an independent timeout.
// If a subscriber fails or times out, the others are unaffected.
type Fanout struct {
	subscribers []port.SubscriberPort
	timeout     time.Duration
	logger      port.LoggerPort
}

// FanoutConfig configures a [Fanout].
type FanoutConfig struct {
	// Subscribers is the list of downstream subscribers.
	Subscribers []port.SubscriberPort

	// Timeout is the maximum time each subscriber has to process a version
	// event. Default: 5s.
	Timeout time.Duration

	// Logger is used to log subscriber failures. Optional.
	Logger port.LoggerPort
}

// NewFanout creates a [Fanout] that dispatches to all given subscribers.
func NewFanout(cfg FanoutConfig) *Fanout {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Fanout{
		subscribers: cfg.Subscribers,
		timeout:     timeout,
		logger:      cfg.Logger,
	}
}

// OnVersion dispatches the record to all subscribers concurrently.
// Each subscriber runs with its own timeout context. Errors from
// individual subscribers are logged but do not propagate.
func (f *Fanout) OnVersion(ctx context.Context, record domain.VersionRecord) error {
	if len(f.subscribers) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	wg.Add(len(f.subscribers))

	for _, sub := range f.subscribers {
		go func(s port.SubscriberPort) {
			defer wg.Done()

			subCtx, cancel := context.WithTimeout(ctx, f.timeout)
			defer cancel()

			if err := s.OnVersion(subCtx, record); err != nil {
				if f.logger != nil {
					f.logger.Warn("audit: subscriber failed",
						"error", err,
						"entity_type", record.EntityType,
						"entity_id", record.EntityID,
						"version", record.Version)
				}
			}
		}(sub)
	}

	wg.Wait()
	return nil
}
