package app

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_ProcessItem(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 2, QueueSize: 10})

	var processed atomic.Bool
	done := make(chan struct{})
	pool.Submit(WorkItem{Process: func(ctx context.Context) {
		processed.Store(true)
		close(done)
	}})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for item to be processed")
	}

	if !processed.Load() {
		t.Error("item was not processed")
	}

	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}

func TestWorkerPool_Concurrency(t *testing.T) {
	const workers = 4
	pool := NewPool(PoolConfig{Workers: workers, QueueSize: 100})

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		pool.Submit(WorkItem{Process: func(ctx context.Context) {
			defer wg.Done()
			c := concurrent.Add(1)
			// Update max concurrent.
			for {
				m := maxConcurrent.Load()
				if c <= m || maxConcurrent.CompareAndSwap(m, c) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond) // Hold the goroutine.
			concurrent.Add(-1)
		}})
	}

	wg.Wait()

	if got := maxConcurrent.Load(); got < 2 {
		t.Errorf("maxConcurrent = %d, want ≥ 2 (submitted %d items to %d workers)", got, workers, workers)
	}

	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}

func TestWorkerPool_Backpressure(t *testing.T) {
	var backpressureCalled atomic.Bool
	pool := NewPool(PoolConfig{
		Workers:   1,
		QueueSize: 1,
		OnBackpressure: func(item WorkItem) {
			backpressureCalled.Store(true)
		},
	})

	// Block the single worker so the channel fills up.
	block := make(chan struct{})
	pool.Submit(WorkItem{Process: func(ctx context.Context) {
		<-block
	}})

	// Fill the queue (capacity 1).
	pool.Submit(WorkItem{Process: func(ctx context.Context) {}})

	// This one should trigger backpressure and then block until space
	// is available (Submit now blocks instead of dropping).
	done := make(chan struct{})
	go func() {
		pool.Submit(WorkItem{Process: func(ctx context.Context) {}})
		close(done)
	}()

	// Give the goroutine a moment to hit backpressure.
	time.Sleep(50 * time.Millisecond)

	if !backpressureCalled.Load() {
		t.Error("backpressure callback was not invoked")
	}

	close(block)

	// Wait for the blocked Submit to complete.
	<-done

	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}

func TestWorkerPool_Shutdown_Drain(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 1, QueueSize: 100})

	var count atomic.Int32

	// Block the worker initially so items accumulate.
	block := make(chan struct{})
	pool.Submit(WorkItem{Process: func(ctx context.Context) {
		<-block
		count.Add(1)
	}})

	// Queue 5 more items while the worker is blocked.
	for i := 0; i < 5; i++ {
		pool.Submit(WorkItem{Process: func(ctx context.Context) {
			count.Add(1)
		}})
	}

	// Unblock and shut down — all queued items should drain.
	close(block)
	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	if got := count.Load(); got != 6 {
		t.Errorf("processed %d items, want 6", got)
	}
}

func TestWorkerPool_ShutdownTimeout(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 1, QueueSize: 10})

	// Submit an item that blocks forever.
	pool.Submit(WorkItem{Process: func(ctx context.Context) {
		<-ctx.Done()
	}})

	// Allow the worker to pick up the item.
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := pool.Shutdown(ctx)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestWorkerPool_NoGoroutineLeak(t *testing.T) {
	// Baseline goroutine count.
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	pool := NewPool(PoolConfig{Workers: 4, QueueSize: 100})

	// Submit and process a few items.
	var processed atomic.Int32
	const total = 20
	for i := 0; i < total; i++ {
		pool.Submit(WorkItem{Process: func(ctx context.Context) {
			processed.Add(1)
		}})
	}

	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	if got := processed.Load(); got != total {
		t.Errorf("processed %d items, want %d", got, total)
	}

	// Allow goroutines to exit.
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	time.Sleep(10 * time.Millisecond)

	final := runtime.NumGoroutine()
	// Allow a small margin for runtime goroutines.
	if final > baseline+2 {
		t.Errorf("goroutine leak: baseline=%d, final=%d", baseline, final)
	}
}

func TestWorkerPool_SubmitAfterShutdown(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 1, QueueSize: 10})
	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	ok := pool.Submit(WorkItem{Process: func(ctx context.Context) {
		t.Error("should not run after shutdown")
	}})
	if ok {
		t.Error("Submit after shutdown returned true, want false")
	}
}

func TestWorkerPool_DoubleShutdown(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 1, QueueSize: 10})
	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("first shutdown error: %v", err)
	}
	if err := pool.Shutdown(context.Background()); err != nil {
		t.Fatalf("second shutdown error: %v", err)
	}
}
