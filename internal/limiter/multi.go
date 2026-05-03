// Package limiter provides a key-based rate limiter that creates one
// RateLimiter instance per unique key (e.g. per client IP, per user ID).
package limiter

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms"
)

const (
	defaultEvictInterval = 5 * time.Minute
	defaultIdleTTL       = 10 * time.Minute
)

// entry pairs a RateLimiter with a last-access timestamp used for idle eviction.
// lastSeen is stored as Unix nanoseconds in an atomic so reads never need a write lock.
type entry struct {
	rl       algorithms.RateLimiter
	lastSeen atomic.Int64
}

func (e *entry) touch() { e.lastSeen.Store(time.Now().UnixNano()) }

// Multi is a thread-safe, key-based rate limiter.
// Each unique key gets its own RateLimiter created by the factory function.
// A background goroutine evicts keys idle for longer than idleTTL to bound memory.
// Call Stop when the Multi is no longer needed to release the goroutine.
type Multi[K comparable] struct {
	mu       sync.RWMutex
	factory  func() algorithms.RateLimiter
	limits   map[K]*entry
	stopOnce sync.Once
	stopCh   chan struct{}
}

// Option configures a Multi limiter.
type Option func(*config)

type config struct {
	evictInterval time.Duration
	idleTTL       time.Duration
}

// WithEvictInterval overrides how often the eviction sweep runs (default: 5 min).
func WithEvictInterval(d time.Duration) Option {
	return func(c *config) { c.evictInterval = d }
}

// WithIdleTTL overrides how long a key must be idle before eviction (default: 10 min).
func WithIdleTTL(d time.Duration) Option {
	return func(c *config) { c.idleTTL = d }
}

// NewMulti returns a Multi with default eviction settings (sweep every 5 min, evict after 10 min idle).
func NewMulti[K comparable](factory func() algorithms.RateLimiter) *Multi[K] {
	return NewMultiWithOptions[K](factory)
}

// NewMultiWithOptions returns a Multi with custom eviction settings.
func NewMultiWithOptions[K comparable](factory func() algorithms.RateLimiter, opts ...Option) *Multi[K] {
	cfg := &config{
		evictInterval: defaultEvictInterval,
		idleTTL:       defaultIdleTTL,
	}
	for _, o := range opts {
		o(cfg)
	}
	m := &Multi[K]{
		factory: factory,
		limits:  make(map[K]*entry),
		stopCh:  make(chan struct{}),
	}
	go m.evictLoop(cfg.evictInterval, cfg.idleTTL)
	return m
}

// Stop shuts down the background eviction goroutine. Safe to call multiple times.
func (m *Multi[K]) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
}

// Allow returns true if one request from key is permitted.
func (m *Multi[K]) Allow(key K) bool {
	return m.get(key).rl.Allow()
}

// AllowN returns true if n requests from key are permitted at once.
func (m *Multi[K]) AllowN(key K, n int) bool {
	return m.get(key).rl.AllowN(n)
}

// Remaining returns the remaining capacity for key.
// Returns -1 if the key has never been seen (no limiter is created — no side effect on read).
func (m *Multi[K]) Remaining(key K) int {
	m.mu.RLock()
	e, ok := m.limits[key]
	m.mu.RUnlock()
	if !ok {
		return -1
	}
	e.touch()
	return e.rl.Remaining()
}

// Reset resets the limiter for a specific key.
func (m *Multi[K]) Reset(key K) {
	m.mu.RLock()
	e, ok := m.limits[key]
	m.mu.RUnlock()
	if ok {
		e.rl.Reset()
	}
}

// Len returns the number of currently tracked keys.
func (m *Multi[K]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.limits)
}

// get returns the entry for key, creating one via factory if it does not exist.
// Uses double-checked locking: optimistic RLock first, write lock only on a miss.
func (m *Multi[K]) get(key K) *entry {
	m.mu.RLock()
	e, ok := m.limits[key]
	m.mu.RUnlock()
	if ok {
		e.touch()
		return e
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Re-check: another goroutine may have created the entry between the two locks.
	if e, ok = m.limits[key]; ok {
		e.touch()
		return e
	}
	e = &entry{rl: m.factory()}
	e.touch()
	m.limits[key] = e
	return e
}

// evictLoop runs on a background goroutine, sweeping the map every interval.
func (m *Multi[K]) evictLoop(interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.evict(ttl)
		case <-m.stopCh:
			return
		}
	}
}

// evict removes keys whose lastSeen is older than ttl.
func (m *Multi[K]) evict(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl).UnixNano()
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.limits {
		if e.lastSeen.Load() < cutoff {
			delete(m.limits, k)
		}
	}
}
