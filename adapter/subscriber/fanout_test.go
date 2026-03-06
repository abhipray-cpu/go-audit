package subscriber

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// recordingSub records calls to OnVersion.
type recordingSub struct {
	mu      sync.Mutex
	records []domain.VersionRecord
}

func (r *recordingSub) OnVersion(_ context.Context, rec domain.VersionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func (r *recordingSub) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

// failingSub always returns an error.
type failingSub struct{ called atomic.Int32 }

func (f *failingSub) OnVersion(_ context.Context, _ domain.VersionRecord) error {
	f.called.Add(1)
	return errors.New("subscriber failed")
}

// slowSub blocks until context expires.
type slowSub struct{ called atomic.Int32 }

func (s *slowSub) OnVersion(ctx context.Context, _ domain.VersionRecord) error {
	s.called.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

// silentLogger satisfies LoggerPort without output.
type silentLogger struct{}

func (l *silentLogger) Debug(_ string, _ ...any) {}
func (l *silentLogger) Info(_ string, _ ...any)  {}
func (l *silentLogger) Warn(_ string, _ ...any)  {}
func (l *silentLogger) Error(_ string, _ ...any) {}

func makeTestRecord() domain.VersionRecord {
	return domain.VersionRecord{
		ID:         "r1",
		EntityType: "user",
		EntityID:   "u1",
		Version:    1,
		Metadata:   domain.VersionMetadata{ActorID: "admin"},
	}
}

func TestFanout_AllSubscribersNotified(t *testing.T) {
	s1 := &recordingSub{}
	s2 := &recordingSub{}
	s3 := &recordingSub{}

	fanout := NewFanout(FanoutConfig{
		Subscribers: []port.SubscriberPort{s1, s2, s3},
		Logger:      &silentLogger{},
	})

	err := fanout.OnVersion(context.Background(), makeTestRecord())
	if err != nil {
		t.Fatalf("OnVersion() error: %v", err)
	}

	for i, s := range []*recordingSub{s1, s2, s3} {
		if s.count() != 1 {
			t.Errorf("subscriber[%d] call count = %d, want 1", i, s.count())
		}
	}
}

func TestFanout_FailureIsolated(t *testing.T) {
	good := &recordingSub{}
	bad := &failingSub{}
	also_good := &recordingSub{}

	fanout := NewFanout(FanoutConfig{
		Subscribers: []port.SubscriberPort{good, bad, also_good},
		Logger:      &silentLogger{},
	})

	err := fanout.OnVersion(context.Background(), makeTestRecord())
	if err != nil {
		t.Fatalf("OnVersion() error: %v (should not propagate sub errors)", err)
	}

	if good.count() != 1 {
		t.Error("good subscriber was not called")
	}
	if also_good.count() != 1 {
		t.Error("also_good subscriber was not called")
	}
	if bad.called.Load() != 1 {
		t.Error("bad subscriber was not called")
	}
}

func TestFanout_TimeoutRespected(t *testing.T) {
	good := &recordingSub{}
	slow := &slowSub{}

	fanout := NewFanout(FanoutConfig{
		Subscribers: []port.SubscriberPort{good, slow},
		Timeout:     50 * time.Millisecond,
		Logger:      &silentLogger{},
	})

	start := time.Now()
	err := fanout.OnVersion(context.Background(), makeTestRecord())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("OnVersion() error: %v", err)
	}

	if good.count() != 1 {
		t.Error("good subscriber was not called")
	}
	if slow.called.Load() != 1 {
		t.Error("slow subscriber was not called")
	}

	// Should complete within ~50ms, not hang forever.
	if elapsed > 500*time.Millisecond {
		t.Errorf("Fanout took %v, expected ~50ms timeout", elapsed)
	}
}

func TestFanout_EmptySubscribers(t *testing.T) {
	fanout := NewFanout(FanoutConfig{})

	err := fanout.OnVersion(context.Background(), makeTestRecord())
	if err != nil {
		t.Fatalf("OnVersion() error: %v", err)
	}
}

// recordingLogger captures log calls.
type recordingLogger struct {
	mu       sync.Mutex
	messages []string
}

func (l *recordingLogger) Debug(msg string, _ ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, msg)
}
func (l *recordingLogger) Info(msg string, _ ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, msg)
}
func (l *recordingLogger) Warn(msg string, _ ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, msg)
}
func (l *recordingLogger) Error(msg string, _ ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, msg)
}

func TestLogSubscriber_LogsVersionCreation(t *testing.T) {
	logger := &recordingLogger{}
	sub := NewLog(logger)

	err := sub.OnVersion(context.Background(), makeTestRecord())
	if err != nil {
		t.Fatalf("OnVersion() error: %v", err)
	}

	logger.mu.Lock()
	defer logger.mu.Unlock()

	if len(logger.messages) != 1 {
		t.Fatalf("expected 1 log message, got %d", len(logger.messages))
	}
	if logger.messages[0] != "audit: version created" {
		t.Errorf("log message = %q, want %q", logger.messages[0], "audit: version created")
	}
}
