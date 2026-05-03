// Package leakybucket implements the leaky bucket rate limiter in its "as a meter" variant.
//
// # Two variants of the leaky bucket
//
// The leaky bucket algorithm has two common interpretations:
//
//   - As a queue (packet shaping): requests enter a physical FIFO queue and are
//     forwarded downstream at a constant output rate. Bursts are absorbed in the
//     queue; excess requests are dropped when it overflows. This is the original
//     algorithm used in network devices and traffic shapers where requests physically
//     wait and are processed asynchronously.
//
//   - As a meter (rate limiting): the queue depth is tracked mathematically.
//     Allow() accepts a request if the queue has space, which drains at a fixed
//     rate over time. No request physically waits — all decisions are immediate.
//     This is the variant implemented here.
//
// # Equivalence with token bucket
//
// For pure accept/reject decisions (Allow() returns bool, caller executes immediately),
// the leaky bucket meter variant is mathematically equivalent to a token bucket with
// the same capacity and rate. The distinction — smooth output rate vs burst absorption —
// only appears when requests are physically queued and forwarded asynchronously, which
// is outside the scope of the RateLimiter interface.
//
// The value of studying both algorithms is understanding the different mental models:
// token bucket asks "do I have budget?"; leaky bucket asks "is there room in the queue?"
// Both yield identical allow/deny decisions at the same capacity and rate.
//
// # Trade-offs
//
//   - Strength: intuitive for capacity planning; natural fit for output-rate-limiting.
//   - Weakness: for pure rate limiting, provides no behavioral advantage over token bucket.
//   - For true output smoothing, use the "as a queue" variant with an async drain goroutine.
package leakybucket

import (
	"sync"
	"time"
)

// LeakyBucket is a rate limiter that tracks a virtual queue depth and drains it over time.
type LeakyBucket struct {
	mu         sync.Mutex
	capacity   int
	queue      int     // current number of in-flight virtual requests
	ratePerSec float64 // drain rate in requests/second
	lastDrain  time.Time
}

// New returns a LeakyBucket with the given queue capacity and drain rate.
func New(capacity int, ratePerSec float64) *LeakyBucket {
	return &LeakyBucket{
		capacity:  capacity,
		ratePerSec: ratePerSec,
		lastDrain: time.Now(),
	}
}

// Allow returns true if there is space in the virtual queue.
func (lb *LeakyBucket) Allow() bool {
	return lb.AllowN(1)
}

// AllowN returns true if n requests fit in the queue after draining elapsed slots.
func (lb *LeakyBucket) AllowN(n int) bool {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.drain()
	if lb.queue+n > lb.capacity {
		return false
	}
	lb.queue += n
	return true
}

// Reset empties the queue and resets the drain timer.
func (lb *LeakyBucket) Reset() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.queue = 0
	lb.lastDrain = time.Now()
}

// Remaining returns the number of free slots currently available in the virtual queue.
func (lb *LeakyBucket) Remaining() int {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.drain()
	return lb.capacity - lb.queue
}

// drain removes slots from the queue proportional to elapsed time.
// Uses integer arithmetic so the drain is discrete and reproducible.
// Must be called with lb.mu held.
func (lb *LeakyBucket) drain() {
	now := time.Now()
	elapsed := now.Sub(lb.lastDrain).Seconds()
	drained := int(elapsed * lb.ratePerSec)
	if drained > 0 {
		lb.queue -= drained
		if lb.queue < 0 {
			lb.queue = 0
		}
		lb.lastDrain = now
	}
}
