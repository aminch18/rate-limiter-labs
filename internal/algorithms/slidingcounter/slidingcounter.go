// Package slidingcounter implements a sliding window counter rate limiter.
// It keeps two fixed-window counters (current and previous) and blends them
// to approximate a sliding window without storing per-request timestamps.
// Formula: estimated = prev_count × (1 - elapsed/window) + curr_count
// Trade-off: O(1) memory like fixed window, near-zero boundary bursts in practice,
// but the result is an approximation — not mathematically exact.
package slidingcounter

import (
	"sync"
	"time"
)

// SlidingCounter is a rate limiter using a weighted two-window counter approximation.
type SlidingCounter struct {
	mu          sync.Mutex
	limit       int
	windowSecs  int
	prevCount   int
	currCount   int
	windowStart time.Time
}

// New returns a SlidingCounter that allows up to limit requests per windowSecs-second window.
func New(limit, windowSecs int) *SlidingCounter {
	return &SlidingCounter{
		limit:       limit,
		windowSecs:  windowSecs,
		windowStart: time.Now(),
	}
}

// Allow returns true if one request is permitted.
func (sc *SlidingCounter) Allow() bool {
	return sc.AllowN(1)
}

// AllowN returns true if n requests are permitted.
func (sc *SlidingCounter) AllowN(n int) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.maybeAdvance()

	window := float64(sc.windowSecs) * float64(time.Second)
	elapsed := float64(time.Since(sc.windowStart))
	weight := 1.0 - elapsed/window
	estimated := float64(sc.prevCount)*weight + float64(sc.currCount)

	if int(estimated)+n > sc.limit {
		return false
	}
	sc.currCount += n
	return true
}

// Reset zeroes both counters and starts a fresh window.
func (sc *SlidingCounter) Reset() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.prevCount = 0
	sc.currCount = 0
	sc.windowStart = time.Now()
}

// Remaining returns an approximation of remaining capacity in the current sliding window.
func (sc *SlidingCounter) Remaining() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.maybeAdvance()
	window := float64(sc.windowSecs) * float64(time.Second)
	elapsed := float64(time.Since(sc.windowStart))
	weight := 1.0 - elapsed/window
	estimated := float64(sc.prevCount)*weight + float64(sc.currCount)
	rem := sc.limit - int(estimated)
	if rem < 0 {
		return 0
	}
	return rem
}

// maybeAdvance rolls the window forward when the current window has expired.
// Advances windowStart by exactly one window duration so elapsed time within
// the new window is preserved — critical for the weighted blend to be accurate.
// If more than two windows have passed (long idle period), both counters reset.
// Must be called with sc.mu held.
func (sc *SlidingCounter) maybeAdvance() {
	windowDur := time.Duration(sc.windowSecs) * time.Second
	if time.Since(sc.windowStart) < windowDur {
		return
	}
	sc.prevCount = sc.currCount
	sc.currCount = 0
	sc.windowStart = sc.windowStart.Add(windowDur)
	// If another full window has also elapsed (long idle), prev is irrelevant too.
	if time.Since(sc.windowStart) >= windowDur {
		sc.prevCount = 0
		sc.windowStart = time.Now()
	}
}
