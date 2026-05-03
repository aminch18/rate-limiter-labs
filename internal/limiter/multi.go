// Package limiter provides a key-based rate limiter that creates one
// RateLimiter instance per unique key (e.g. per client IP, per user ID).
package limiter

import (
	"sync"

	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms"
)

// Multi is a thread-safe, key-based rate limiter.
// Each unique key gets its own RateLimiter created by the factory function.
//
// Keys are never evicted. For long-lived servers with high key cardinality
// (e.g. arbitrary public IPs), add an eviction strategy such as an LRU cache
// or a TTL sweep before deploying in production.
type Multi[K comparable] struct {
	mu      sync.RWMutex
	factory func() algorithms.RateLimiter
	limits  map[K]algorithms.RateLimiter
}

// NewMulti returns a Multi that creates a new RateLimiter per unseen key using factory.
func NewMulti[K comparable](factory func() algorithms.RateLimiter) *Multi[K] {
	return &Multi[K]{
		factory: factory,
		limits:  make(map[K]algorithms.RateLimiter),
	}
}

// Allow returns true if one request from key is permitted.
func (m *Multi[K]) Allow(key K) bool {
	return m.get(key).Allow()
}

// AllowN returns true if n requests from key are permitted at once.
func (m *Multi[K]) AllowN(key K, n int) bool {
	return m.get(key).AllowN(n)
}

// Remaining returns the remaining capacity for key's limiter.
// Returns -1 if the key has never been seen — callers should treat -1 as
// "full capacity available but not yet measured". This avoids the side effect
// of creating a limiter entry on a read-only operation.
//
// In normal usage (Allow followed by Remaining for the same key) -1 never occurs.
func (m *Multi[K]) Remaining(key K) int {
	m.mu.RLock()
	rl, ok := m.limits[key]
	m.mu.RUnlock()
	if !ok {
		return -1
	}
	return rl.Remaining()
}

// Reset resets the limiter for a specific key.
func (m *Multi[K]) Reset(key K) {
	m.get(key).Reset()
}

// Len returns the number of tracked keys.
func (m *Multi[K]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.limits)
}

// get returns the limiter for key, creating one via factory if it does not exist.
// Uses double-checked locking to avoid unnecessary write-lock contention.
func (m *Multi[K]) get(key K) algorithms.RateLimiter {
	m.mu.RLock()
	rl, ok := m.limits[key]
	m.mu.RUnlock()
	if ok {
		return rl
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if rl, ok = m.limits[key]; ok {
		return rl
	}
	rl = m.factory()
	m.limits[key] = rl
	return rl
}
