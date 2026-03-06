package audittest

import (
	"testing"
	"time"
)

func TestMockClock_FixedTime(t *testing.T) {
	fixed := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	clock := NewMockClock(fixed)

	if got := clock.Now(); !got.Equal(fixed) {
		t.Errorf("Now() = %v, want %v", got, fixed)
	}

	// Multiple calls return the same time.
	if got := clock.Now(); !got.Equal(fixed) {
		t.Errorf("second Now() = %v, want %v", got, fixed)
	}
}

func TestMockClock_Advance(t *testing.T) {
	fixed := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewMockClock(fixed)

	clock.Advance(24 * time.Hour)
	expected := fixed.Add(24 * time.Hour)
	if got := clock.Now(); !got.Equal(expected) {
		t.Errorf("after Advance: Now() = %v, want %v", got, expected)
	}

	// Advance again.
	clock.Advance(30 * time.Minute)
	expected = expected.Add(30 * time.Minute)
	if got := clock.Now(); !got.Equal(expected) {
		t.Errorf("after second Advance: Now() = %v, want %v", got, expected)
	}
}

func TestMockClock_Set(t *testing.T) {
	clock := NewMockClock(time.Now())

	target := time.Date(2030, 12, 31, 23, 59, 59, 0, time.UTC)
	clock.Set(target)

	if got := clock.Now(); !got.Equal(target) {
		t.Errorf("after Set: Now() = %v, want %v", got, target)
	}
}
