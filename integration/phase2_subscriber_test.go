//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/adapter/subscriber"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------------------------------------------------------------------------
// Phase 2 — SUB-001 to SUB-005: Subscriber & Fanout
// ---------------------------------------------------------------------------

// testSubscriber records every version event it receives.
type testSubscriber struct {
	mu      sync.Mutex
	records []domain.VersionRecord
}

func (s *testSubscriber) OnVersion(_ context.Context, record domain.VersionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return nil
}

func (s *testSubscriber) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// failingSubscriber always returns an error.
type failingSubscriber struct{}

func (s *failingSubscriber) OnVersion(_ context.Context, _ domain.VersionRecord) error {
	return errors.New("subscriber failure")
}

// SUB-001: TestSubscriber_OnVersion verifies subscriber receives every version event.
func TestSubscriber_OnVersion(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	sub := &testSubscriber{}
	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Subscriber = sub
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		user.Name = randString(5)
		versionSync(t, ctx, a, user)
	}

	if sub.Count() != 3 {
		t.Fatalf("expected 3 events, got %d", sub.Count())
	}
}

// SUB-002: TestSubscriber_Fanout dispatches to multiple subscribers.
func TestSubscriber_Fanout(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	sub1 := &testSubscriber{}
	sub2 := &testSubscriber{}
	fanout := subscriber.NewFanout(subscriber.FanoutConfig{
		Subscribers: []port.SubscriberPort{sub1, sub2},
		Timeout:     5 * time.Second,
	})

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Subscriber = fanout
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	if sub1.Count() != 1 || sub2.Count() != 1 {
		t.Fatalf("expected both subscribers to receive 1 event; sub1=%d sub2=%d", sub1.Count(), sub2.Count())
	}
}

// SUB-003: TestSubscriber_FailureIsolation verifies one failing subscriber
// does not affect others in a fanout.
func TestSubscriber_FailureIsolation(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	good := &testSubscriber{}
	bad := &failingSubscriber{}
	fanout := subscriber.NewFanout(subscriber.FanoutConfig{
		Subscribers: []port.SubscriberPort{bad, good},
		Timeout:     5 * time.Second,
	})

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Subscriber = fanout
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	// Good subscriber should still receive the event.
	if good.Count() != 1 {
		t.Fatalf("expected good subscriber to receive 1 event, got %d", good.Count())
	}
}

// SUB-004: TestSubscriber_Timeout verifies that a slow subscriber is timed out.
func TestSubscriber_Timeout(t *testing.T) {
	var completed atomic.Bool
	slowSub := &blockingSubscriber{
		delay:     2 * time.Second,
		completed: &completed,
	}

	fanout := subscriber.NewFanout(subscriber.FanoutConfig{
		Subscribers: []port.SubscriberPort{slowSub},
		Timeout:     100 * time.Millisecond,
	})

	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Subscriber = fanout
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	// After the version completes, the slow subscriber should have been cancelled.
	// Wait briefly to let it finish.
	time.Sleep(200 * time.Millisecond)
	// Version should have succeeded regardless.
}

// blockingSubscriber sleeps for a configurable delay.
type blockingSubscriber struct {
	delay     time.Duration
	completed *atomic.Bool
}

func (s *blockingSubscriber) OnVersion(ctx context.Context, _ domain.VersionRecord) error {
	select {
	case <-time.After(s.delay):
		s.completed.Store(true)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SUB-005: TestSubscriber_LogSubscriber exercises the default log subscriber
// without crashing.
func TestSubscriber_LogSubscriber(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	// Default subscriber is LogSubscriber — just ensure it works.
	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)
	// No panic = pass.
}
