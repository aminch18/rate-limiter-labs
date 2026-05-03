// Package slidinglog implements a sliding window log rate limiter.
// Every request timestamp is stored; on each call the log is pruned of entries
// older than the window, then the remaining count is checked against the limit.
// Trade-off: mathematically precise (no boundary bursts), but O(n) memory per client
// where n = requests per window — expensive at high request rates.
package slidinglog

import (
	"sync"
	"time"
)

// SlidingLog is a rate limiter that maintains a timestamped log of accepted requests.
type SlidingLog struct {
	mu         sync.Mutex
	limit      int
	windowSecs int
	log        []time.Time
}

// New returns a SlidingLog that allows up to limit requests per windowSecs-second sliding window.
//
// The log starts empty and grows on demand via Go's standard slice-doubling strategy.
// Peak memory at steady state is roughly (requests_per_window × 24 bytes) per client;
// this is the primary cost driver at scale, not per-operation allocations.
func New(limit, windowSecs int) *SlidingLog {
	return &SlidingLog{
		limit:      limit,
		windowSecs: windowSecs,
		log:        make([]time.Time, 0),
	}
}

// Allow returns true if one request is permitted within the current sliding window.
func (sl *SlidingLog) Allow() bool {
	return sl.AllowN(1)
}

// AllowN returns true if n requests are permitted within the current sliding window.
func (sl *SlidingLog) AllowN(n int) bool {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	now := time.Now()
	sl.purge(now)

	if len(sl.log)+n > sl.limit {
		return false
	}
	for i := 0; i < n; i++ {
		sl.log = append(sl.log, now)
	}
	return true
}

// Reset clears all recorded timestamps.
func (sl *SlidingLog) Reset() {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.log = sl.log[:0]
}

// Remaining returns the number of requests still permitted in the current sliding window.
func (sl *SlidingLog) Remaining() int {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.purge(time.Now())
	return sl.limit - len(sl.log)
}

// purge removes timestamps older than the window from the front of the log.
// Must be called with sl.mu held.
func (sl *SlidingLog) purge(now time.Time) {
	cutoff := now.Add(-time.Duration(sl.windowSecs) * time.Second)
	i := 0
	for i < len(sl.log) && sl.log[i].Before(cutoff) {
		i++
	}
	sl.log = sl.log[i:]
}
