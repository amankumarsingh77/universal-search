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
	// unpauseCh is closed (and replaced with a fresh channel) whenever the
	// pause state transitions from "active" to "cleared". Callers waiting for
	// an unpause event select on this channel together with a deadline timer
	// and ctx.Done(). The channel itself is never sent to — only closed —
	// so all concurrent waiters wake simultaneously (broadcast semantics).
	unpauseCh chan struct{}
}

// NewRateLimiter creates a rate limiter that allows maxReqs requests per window.
func NewRateLimiter(maxReqs int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		tokens:    make([]time.Time, 0, maxReqs),
		maxReqs:   maxReqs,
		window:    window,
		unpauseCh: make(chan struct{}),
	}
}

// broadcastUnpause closes the current unpauseCh and replaces it with a fresh
// one. Must be called with rl.mu held.
func (rl *RateLimiter) broadcastUnpause() {
	close(rl.unpauseCh)
	rl.unpauseCh = make(chan struct{})
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

// PauseUntil sets a global pause until t. If t extends the current pauseUntil,
// the deadline is updated (waiters stay blocked). If t is in the past (an
// explicit clear), the pause is cleared and all WaitForUnpause callers are
// woken via a channel broadcast. If t is before the current pauseUntil and not
// in the past, it is ignored (later deadline wins).
func (rl *RateLimiter) PauseUntil(t time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	if !t.After(now) {
		// Past or zero: explicit clear — wake all waiters if previously paused.
		if !rl.pauseUntil.IsZero() && rl.pauseUntil.After(now) {
			rl.pauseUntil = t
			rl.broadcastUnpause()
		} else {
			rl.pauseUntil = t
		}
		return
	}
	if t.After(rl.pauseUntil) {
		rl.pauseUntil = t
	}
}

// SetRatePerMinute updates the configured maximum requests per window. Values
// less than 1 are ignored. Existing in-window tokens are retained, so a
// reduction takes effect against future Allow calls only — already-admitted
// requests are not retroactively revoked.
func (rl *RateLimiter) SetRatePerMinute(maxReqs int) {
	if maxReqs < 1 {
		return
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.maxReqs = maxReqs
}

// Stats returns the number of tokens used inside the current sliding window
// alongside the configured max. Used to surface rate-limit headroom in the UI.
func (rl *RateLimiter) Stats() (used, max int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.window)
	valid := 0
	for _, t := range rl.tokens {
		if t.After(cutoff) {
			valid++
		}
	}
	return valid, rl.maxReqs
}

// PausedUntil returns the current pause deadline (zero if not paused).
func (rl *RateLimiter) PausedUntil() time.Time {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.pauseUntil
}

// WaitForUnpause blocks until the current quota pause has expired (deadline
// reached) or has been explicitly cleared via PauseUntil(pastTime), then
// returns nil. Returns immediately with nil if no pause is active. Returns
// ctx.Err() if the context is cancelled while waiting.
//
// If PauseUntil is called to extend an active pause while this goroutine is
// waiting, the goroutine stays blocked until the new (later) deadline.
//
// All concurrent callers are notified simultaneously when the pause clears
// (broadcast semantics via close-and-replace channel).
func (rl *RateLimiter) WaitForUnpause(ctx context.Context) error {
	for {
		rl.mu.Lock()
		until := rl.pauseUntil
		ch := rl.unpauseCh
		rl.mu.Unlock()

		if until.IsZero() || !time.Now().Before(until) {
			return nil
		}

		remaining := time.Until(until)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
			// Pause was cleared or deadline fired via broadcastUnpause;
			// re-check in case it was replaced by a new (extended) pause.
		case <-time.After(remaining):
			// Deadline reached; broadcast to wake any other waiters and
			// replace the channel so future pauses work correctly.
			rl.mu.Lock()
			// Only broadcast if the pauseUntil hasn't changed (another
			// goroutine may have extended it while we were sleeping).
			if !rl.pauseUntil.After(time.Now()) {
				rl.broadcastUnpause()
			}
			rl.mu.Unlock()
		}
	}
}

// WaitIfPaused blocks until the global pause expires or ctx is cancelled.
func (rl *RateLimiter) WaitIfPaused(ctx context.Context) error {
	return rl.WaitForUnpause(ctx)
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
