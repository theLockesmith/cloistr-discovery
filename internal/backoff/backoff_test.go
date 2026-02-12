package backoff

import (
	"testing"
	"time"
)

func TestBackoff_Next(t *testing.T) {
	b := New(1*time.Second, 10*time.Second, 2.0)

	// First call should return ~1s (with jitter)
	d1 := b.Next()
	if d1 < 800*time.Millisecond || d1 > 1200*time.Millisecond {
		t.Errorf("expected ~1s, got %v", d1)
	}

	// Second call should return ~2s
	d2 := b.Next()
	if d2 < 1600*time.Millisecond || d2 > 2400*time.Millisecond {
		t.Errorf("expected ~2s, got %v", d2)
	}

	// Third call should return ~4s
	d3 := b.Next()
	if d3 < 3200*time.Millisecond || d3 > 4800*time.Millisecond {
		t.Errorf("expected ~4s, got %v", d3)
	}
}

func TestBackoff_Max(t *testing.T) {
	b := New(1*time.Second, 5*time.Second, 2.0)

	// Exhaust the backoff to hit the max
	for i := 0; i < 10; i++ {
		b.Next()
	}

	// Should be capped at max
	d := b.Next()
	if d < 4*time.Second || d > 6*time.Second {
		t.Errorf("expected ~5s (max), got %v", d)
	}
}

func TestBackoff_Reset(t *testing.T) {
	b := New(1*time.Second, 10*time.Second, 2.0)

	// Advance a few times
	b.Next()
	b.Next()
	b.Next()

	// Reset
	b.Reset()

	// Should be back to initial
	d := b.Next()
	if d < 800*time.Millisecond || d > 1200*time.Millisecond {
		t.Errorf("expected ~1s after reset, got %v", d)
	}
}

func TestTracker_Get(t *testing.T) {
	tracker := NewTracker(1*time.Second, 10*time.Second, 2.0)

	// Get should create a new backoff
	b1 := tracker.Get("relay1")
	if b1 == nil {
		t.Fatal("expected backoff, got nil")
	}

	// Same key should return same backoff
	b2 := tracker.Get("relay1")
	if b1 != b2 {
		t.Error("expected same backoff instance")
	}

	// Different key should return different backoff
	b3 := tracker.Get("relay2")
	if b1 == b3 {
		t.Error("expected different backoff instance")
	}
}

func TestTracker_Reset(t *testing.T) {
	tracker := NewTracker(1*time.Second, 10*time.Second, 2.0)

	b := tracker.Get("relay1")
	b.Next()
	b.Next()

	tracker.Reset("relay1")

	// Should be back to initial
	d := b.Next()
	if d < 800*time.Millisecond || d > 1200*time.Millisecond {
		t.Errorf("expected ~1s after reset, got %v", d)
	}
}
