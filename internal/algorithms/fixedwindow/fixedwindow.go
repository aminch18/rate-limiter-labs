// Package fixedwindow implements a fixed window counter rate limiter.
// Time is divided into windows of fixed duration; each window has an independent counter.
// Trade-off: O(1) memory and trivially simple, but vulnerable to boundary bursts
// where a client can send 2×limit requests by straddling two consecutive windows.
package fixedwindow

import (
	"sync"
	"time"
)

// FixedWindow is a rate limiter that counts requests in fixed time windows.
type FixedWindow struct {
	mu          sync.Mutex
	limit       int
	windowSecs  int
	count       int
	windowStart time.Time
}

// New returns a FixedWindow that allows up to limit requests per windowSecs-second window.
func New(limit, windowSecs int) *FixedWindow {
	return &FixedWindow{
		limit:       limit,
		windowSecs:  windowSecs,
		count:       0,
		windowStart: time.Now(),
	}
}

// Allow returns true if one request is permitted in the current window.
func (fw *FixedWindow) Allow() bool {
	return fw.AllowN(1)
}

// AllowN returns true if n requests are permitted in the current window.
func (fw *FixedWindow) AllowN(n int) bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	fw.maybeReset()
	if fw.count+n > fw.limit {
		return false
	}
	fw.count += n
	return true
}

// Reset resets the counter and starts a new window from now.
func (fw *FixedWindow) Reset() {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.count = 0
	fw.windowStart = time.Now()
}

// Remaining returns the number of requests still permitted in the current window.
func (fw *FixedWindow) Remaining() int {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.maybeReset()
	return fw.limit - fw.count
}

// maybeReset rolls the window forward if the current window has expired.
// Must be called with fw.mu held.
func (fw *FixedWindow) maybeReset() {
	if time.Since(fw.windowStart) >= time.Duration(fw.windowSecs)*time.Second {
		fw.count = 0
		fw.windowStart = time.Now()
	}
}
