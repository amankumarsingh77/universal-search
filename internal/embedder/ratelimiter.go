package embedder

import (
	"context"
	"sync"
	"time"
)

// RateLimiter implements a sliding window rate limiter.
type RateLimiter struct {
	mu         sync.Mutex
	tokens     []time.Time
	maxReqs    int
	window     time.Duration
	pauseUntil time.Time
}

// NewRateLimiter creates a rate limiter that allows maxReqs requests per window.
func NewRateLimiter(maxReqs int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		tokens:  make([]time.Time, 0, maxReqs),
		maxReqs: maxReqs,
		window:  window,
	}
}

// Allow returns true if the request is within the rate limit, false otherwise.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Remove expired tokens.
	valid := rl.tokens[:0]
	for _, t := range rl.tokens {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	rl.tokens = valid

	if len(rl.tokens) >= rl.maxReqs {
		return false
	}
	rl.tokens = append(rl.tokens, now)
	return true
}

// PauseUntil sets a global pause until t. If t is before the current pauseUntil, it is ignored.
func (rl *RateLimiter) PauseUntil(t time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if t.After(rl.pauseUntil) {
		rl.pauseUntil = t
	}
}

// PausedUntil returns the current pause deadline (zero if not paused).
func (rl *RateLimiter) PausedUntil() time.Time {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.pauseUntil
}

// WaitIfPaused blocks until the global pause expires or ctx is cancelled.
func (rl *RateLimiter) WaitIfPaused(ctx context.Context) error {
	for {
		rl.mu.Lock()
		until := rl.pauseUntil
		rl.mu.Unlock()
		if until.IsZero() || time.Now().After(until) {
			return nil
		}
		remaining := time.Until(until)
		sleep := remaining
		if sleep > 100*time.Millisecond {
			sleep = 100 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}

// Wait blocks until a request is both un-paused and admitted by the rate
// limiter, or ctx is cancelled. The pause check runs inside the admission loop
// so a PauseUntil() call that lands after an earlier precheck still gates
// subsequent Allow() attempts — without this, a worker could sneak a request
// through during a shared backoff window.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	for {
		if err := rl.WaitIfPaused(ctx); err != nil {
			return err
		}
		if rl.Allow() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
