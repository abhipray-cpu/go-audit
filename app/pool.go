package app

import (
	"context"
	"sync"
	"sync/atomic"
)

// WorkItem represents a unit of work submitted to the [Pool].
type WorkItem struct {
	// Process is the function executed by a worker goroutine.
	// It receives a context that is cancelled when the pool shuts down.
	Process func(ctx context.Context)
}

// BackpressureFunc is called when the pool's work channel is full.
// Implementations may log, record metrics, or switch to synchronous mode.
type BackpressureFunc func(item WorkItem)

// PoolConfig configures a [Pool].
type PoolConfig struct {
	// Workers is the number of concurrent worker goroutines.
	// Must be ≥ 1.
	Workers int

	// QueueSize is the capacity of the buffered work channel.
	// Must be ≥ 1.
	QueueSize int

	// OnBackpressure is called when the work channel is full and a
	// non-blocking submit cannot enqueue the item. If nil, the item
	// is silently dropped.
	OnBackpressure BackpressureFunc
}

// Pool is a bounded goroutine pool with a buffered work channel,
// backpressure callback, and graceful shutdown.
type Pool struct {
	cfg     PoolConfig
	work    chan WorkItem
	cancel  context.CancelFunc
	ctx     context.Context
	wg      sync.WaitGroup
	stopped atomic.Bool
}

// NewPool creates and starts a [Pool] with the given configuration.
// Workers begin consuming from the work channel immediately.
func NewPool(cfg PoolConfig) *Pool {
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.QueueSize < 1 {
		cfg.QueueSize = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		cfg:    cfg,
		work:   make(chan WorkItem, cfg.QueueSize),
		cancel: cancel,
		ctx:    ctx,
	}

	p.wg.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go p.worker()
	}

	return p
}

// Submit enqueues a [WorkItem] for processing. If the work channel is
// full, the configured [BackpressureFunc] is invoked (for logging/metrics)
// and Submit blocks until space is available or the pool is shut down.
// Submit is safe to call from multiple goroutines.
//
// Returns false if the pool has been shut down.
func (p *Pool) Submit(item WorkItem) bool {
	if p.stopped.Load() {
		return false
	}

	select {
	case p.work <- item:
		return true
	default:
		// Channel full — notify backpressure (logging/metrics only).
		if p.cfg.OnBackpressure != nil {
			p.cfg.OnBackpressure(item)
		}
		// Block until space is available or pool shuts down.
		select {
		case p.work <- item:
			return true
		case <-p.ctx.Done():
			return false
		}
	}
}

// Shutdown signals the pool to stop accepting new work, then waits for
// all in-flight items in the channel to be drained. The provided context
// controls the maximum time to wait; if the deadline is exceeded, workers
// are cancelled and Shutdown returns ctx.Err().
func (p *Pool) Shutdown(ctx context.Context) error {
	if p.stopped.Swap(true) {
		return nil // already shut down
	}

	// Close the channel so workers drain remaining items and exit.
	close(p.work)

	// Wait for workers to finish or context to expire.
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.cancel()
		return nil
	case <-ctx.Done():
		// Deadline exceeded — cancel the worker context so in-progress
		// items can detect cancellation.
		p.cancel()
		return ctx.Err()
	}
}

// Pending returns the number of items currently queued (not yet picked
// up by a worker).
func (p *Pool) Pending() int {
	return len(p.work)
}

// worker is the loop executed by each worker goroutine.
func (p *Pool) worker() {
	defer p.wg.Done()
	for item := range p.work {
		item.Process(p.ctx)
	}
}
