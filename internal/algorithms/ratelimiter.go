// Package algorithms defines the common interface for all rate limiting implementations.
package algorithms

import (
	"context"
	"time"
)

// RateLimiter is the single interface all rate limiting algorithms implement.
// All implementations must be safe for concurrent use.
type RateLimiter interface {
	// Allow returns true if a single request is permitted under the current rate limit.
	Allow() bool

	// AllowN returns true if n requests are permitted at once.
	// Useful for batch operations.
	AllowN(n int) bool

	// Reset resets the limiter to its initial state.
	// Useful in tests and benchmarks between runs.
	Reset()

	// Remaining returns the number of requests still permitted under the current state.
	// The value is exact for counter-based algorithms and approximate for blended windows.
	Remaining() int
}

// Wait blocks until rl grants one token or ctx is cancelled.
// Poll interval starts at 1 ms and doubles up to 100 ms to reduce CPU spin
// under sustained load without adding noticeable latency at low rates.
// Returns nil when a token is granted, ctx.Err() if the context is cancelled first.
//
// This is a package-level function rather than an interface method so that
// all existing RateLimiter implementations work without modification — the
// blocking behaviour is composed on top of the non-blocking Allow().
func Wait(ctx context.Context, rl RateLimiter) error {
	const (
		minPoll = 1 * time.Millisecond
		maxPoll = 100 * time.Millisecond
	)
	poll := minPoll
	for {
		if rl.Allow() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
			if poll < maxPoll {
				poll *= 2
			}
		}
	}
}
