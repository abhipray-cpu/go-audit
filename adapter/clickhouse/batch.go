package clickhouse

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// BatchConfig controls the batch buffer behaviour.
type BatchConfig struct {
	// BatchSize is the number of records that triggers an immediate flush.
	// Must be ≥ 1. Default: 1000.
	BatchSize int

	// FlushInterval is the maximum time between flushes even when the
	// batch size has not been reached. Default: 5s.
	FlushInterval time.Duration

	// MaxRetries is the number of retry attempts when a flush fails.
	// Default: 3.
	MaxRetries int

	// BaseBackoff is the base delay for exponential backoff on retry.
	// Default: 100ms.
	BaseBackoff time.Duration

	// Logger is used for structured internal logging. Optional.
	Logger port.LoggerPort
}

// BatchBuffer accumulates version records and flushes them to an OLAPPort
// backend either when the size threshold is reached or when the flush timer
// fires — whichever comes first. Failed flushes are retried with
// exponential backoff.
type BatchBuffer struct {
	sink   port.OLAPPort
	cfg    BatchConfig
	ch     chan domain.VersionRecord
	done   chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
	logger port.LoggerPort
}

// NewBatchBuffer creates a [BatchBuffer] that drains records into sink.
// Call [BatchBuffer.Close] to flush remaining records and stop the
// background goroutine.
func NewBatchBuffer(sink port.OLAPPort, cfg BatchConfig) *BatchBuffer {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 100 * time.Millisecond
	}

	b := &BatchBuffer{
		sink:   sink,
		cfg:    cfg,
		ch:     make(chan domain.VersionRecord, cfg.BatchSize*2),
		done:   make(chan struct{}),
		logger: cfg.Logger,
	}

	b.wg.Add(1)
	go b.loop()
	return b
}

// Enqueue adds a record to the buffer. It blocks only if the internal
// channel is full. After [BatchBuffer.Close] is called, Enqueue panics.
func (b *BatchBuffer) Enqueue(record domain.VersionRecord) {
	b.ch <- record
}

// Close stops the flush loop, drains remaining buffered records, and
// performs a final flush. It is safe to call multiple times.
func (b *BatchBuffer) Close() {
	b.once.Do(func() {
		close(b.ch)
		b.wg.Wait()
	})
}

// loop is the background goroutine that accumulates records and flushes
// them on size threshold or timer tick.
func (b *BatchBuffer) loop() {
	defer b.wg.Done()

	buf := make([]domain.VersionRecord, 0, b.cfg.BatchSize)
	ticker := time.NewTicker(b.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case rec, ok := <-b.ch:
			if !ok {
				// Channel closed — flush remaining records.
				if len(buf) > 0 {
					b.flush(buf)
				}
				return
			}
			buf = append(buf, rec)
			if len(buf) >= b.cfg.BatchSize {
				b.flush(buf)
				buf = buf[:0]
			}

		case <-ticker.C:
			if len(buf) > 0 {
				b.flush(buf)
				buf = buf[:0]
			}
		}
	}
}

// flush sends the batch to the OLAP sink with retry + exponential backoff.
func (b *BatchBuffer) flush(records []domain.VersionRecord) {
	// Copy the slice to avoid aliasing issues on retry.
	batch := make([]domain.VersionRecord, len(records))
	copy(batch, records)

	ctx := context.Background()

	for attempt := 0; attempt <= b.cfg.MaxRetries; attempt++ {
		if err := b.sink.BatchInsert(ctx, batch); err != nil {
			if b.logger != nil {
				b.logger.Warn("audit: batch flush failed",
					"attempt", attempt+1,
					"records", len(batch),
					"error", err,
				)
			}
			if attempt < b.cfg.MaxRetries {
				backoff := time.Duration(math.Pow(2, float64(attempt))) * b.cfg.BaseBackoff
				time.Sleep(backoff)
				continue
			}
			// Exhausted retries — records are lost (caller should use WAL
			// or DLQ for stronger guarantees).
			if b.logger != nil {
				b.logger.Error("audit: batch flush exhausted retries, dropping records",
					"records", len(batch),
				)
			}
			return
		}
		// Success.
		return
	}
}
