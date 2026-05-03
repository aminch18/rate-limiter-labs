// Package tokenbucket implements a token bucket rate limiter.
// Tokens accumulate at a fixed rate up to a maximum capacity.
// Each request consumes one token; AllowN consumes n tokens.
// Bursts up to capacity are naturally supported.
package tokenbucket

import (
	"sync"
	"time"
)

// TokenBucket is a rate limiter backed by the token bucket algorithm.
type TokenBucket struct {
	mu          sync.Mutex
	capacity    float64
	tokens      float64
	ratePerSec  float64
	lastRefill  time.Time
}

// New returns a TokenBucket that allows up to capacity tokens, refilling at ratePerSec tokens/second.
func New(capacity int, ratePerSec float64) *TokenBucket {
	return &TokenBucket{
		capacity:   float64(capacity),
		tokens:     float64(capacity),
		ratePerSec: ratePerSec,
		lastRefill: time.Now(),
	}
}

// Allow returns true if one request is permitted.
func (tb *TokenBucket) Allow() bool {
	return tb.AllowN(1)
}

// AllowN returns true if n requests are permitted at once.
func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	if tb.tokens < float64(n) {
		return false
	}
	tb.tokens -= float64(n)
	return true
}

// Reset restores the bucket to full capacity.
func (tb *TokenBucket) Reset() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.tokens = tb.capacity
	tb.lastRefill = time.Now()
}

// Remaining returns the number of tokens currently available in the bucket.
func (tb *TokenBucket) Remaining() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return int(tb.tokens)
}

// refill adds tokens based on elapsed time since the last refill.
// Must be called with tb.mu held.
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.ratePerSec
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastRefill = now
}
