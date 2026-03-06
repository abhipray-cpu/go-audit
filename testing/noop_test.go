package audittest

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

func TestNoopWAL(t *testing.T) {
	w := &NoopWAL{}

	if err := w.Append(context.Background(), port.WALEntry{}); err != nil {
		t.Errorf("Append() error: %v", err)
	}
	if err := w.Ack(context.Background(), "id1"); err != nil {
		t.Errorf("Ack() error: %v", err)
	}
	entries, err := w.Replay(context.Background())
	if err != nil {
		t.Errorf("Replay() error: %v", err)
	}
	if entries != nil {
		t.Errorf("Replay() = %v, want nil", entries)
	}
}

func TestNoopCache(t *testing.T) {
	c := &NoopCache{}

	rec, ok := c.Get(context.Background(), "user", "u1", 1)
	if ok {
		t.Error("Get() should return false")
	}
	if rec.ID != "" {
		t.Error("Get() should return zero record")
	}

	// Put and Invalidate should not panic.
	c.Put(context.Background(), domain.VersionRecord{})
	c.Invalidate(context.Background(), "user", "u1")
}

func TestNoopInstrumenter(t *testing.T) {
	inst := &NoopInstrumenter{}

	ctx, span := inst.StartSpan(context.Background(), "op")
	if ctx == nil {
		t.Error("StartSpan() returned nil context")
	}
	if span == nil {
		t.Error("StartSpan() returned nil span")
	}

	// Should not panic.
	span.End()
	span.SetAttribute("key", "val")
	inst.EndSpan(span)
	inst.RecordMetric(context.Background(), "metric", 1.0, nil)
}

func TestNoopSubscriber(t *testing.T) {
	sub := &NoopSubscriber{}

	if err := sub.OnVersion(context.Background(), domain.VersionRecord{}); err != nil {
		t.Errorf("OnVersion() error: %v", err)
	}
}
