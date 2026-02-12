// Package backoff provides exponential backoff utilities for retry logic.
package backoff

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// Backoff implements exponential backoff with jitter.
type Backoff struct {
	initial time.Duration
	max     time.Duration
	factor  float64

	mu      sync.Mutex
	current time.Duration
}

// New creates a new Backoff with the given parameters.
// - initial: starting delay (e.g., 30s)
// - max: maximum delay cap (e.g., 30m)
// - factor: multiplier for each retry (e.g., 2.0 for doubling)
func New(initial, max time.Duration, factor float64) *Backoff {
	return &Backoff{
		initial: initial,
		max:     max,
		factor:  factor,
		current: initial,
	}
}

// Default returns a Backoff with sensible defaults:
// - initial: 30 seconds
// - max: 30 minutes
// - factor: 2.0 (doubles each time)
func Default() *Backoff {
	return New(30*time.Second, 30*time.Minute, 2.0)
}

// Next returns the next backoff duration and advances the internal state.
// Includes up to 20% jitter to prevent thundering herd.
func (b *Backoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	duration := b.current

	// Apply jitter: +/- 20%
	jitter := float64(duration) * 0.2 * (rand.Float64()*2 - 1)
	duration = time.Duration(float64(duration) + jitter)

	// Advance for next call
	b.current = time.Duration(float64(b.current) * b.factor)
	if b.current > b.max {
		b.current = b.max
	}

	return duration
}

// Reset resets the backoff to the initial duration.
// Call this after a successful operation.
func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = b.initial
}

// Current returns the current backoff duration without advancing.
func (b *Backoff) Current() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.current
}

// Wait blocks for the next backoff duration or until context is cancelled.
// Returns true if the wait completed, false if context was cancelled.
func (b *Backoff) Wait(ctx context.Context) bool {
	duration := b.Next()
	select {
	case <-ctx.Done():
		return false
	case <-time.After(duration):
		return true
	}
}

// Tracker manages backoff state for multiple keys (e.g., relay URLs).
type Tracker struct {
	initial time.Duration
	max     time.Duration
	factor  float64

	mu       sync.RWMutex
	backoffs map[string]*Backoff
}

// NewTracker creates a tracker for managing per-key backoffs.
func NewTracker(initial, max time.Duration, factor float64) *Tracker {
	return &Tracker{
		initial:  initial,
		max:      max,
		factor:   factor,
		backoffs: make(map[string]*Backoff),
	}
}

// DefaultTracker returns a Tracker with sensible defaults.
func DefaultTracker() *Tracker {
	return NewTracker(30*time.Second, 30*time.Minute, 2.0)
}

// Get returns the Backoff for a given key, creating one if needed.
func (t *Tracker) Get(key string) *Backoff {
	t.mu.RLock()
	b, ok := t.backoffs[key]
	t.mu.RUnlock()

	if ok {
		return b
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Double-check after acquiring write lock
	if b, ok := t.backoffs[key]; ok {
		return b
	}

	b = New(t.initial, t.max, t.factor)
	t.backoffs[key] = b
	return b
}

// Reset resets the backoff for a given key.
func (t *Tracker) Reset(key string) {
	t.mu.RLock()
	b, ok := t.backoffs[key]
	t.mu.RUnlock()

	if ok {
		b.Reset()
	}
}

// Remove deletes the backoff state for a key.
func (t *Tracker) Remove(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.backoffs, key)
}
