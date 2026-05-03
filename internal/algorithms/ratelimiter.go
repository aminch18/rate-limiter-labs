// Package algorithms defines the common interface for all rate limiting implementations.
package algorithms

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
