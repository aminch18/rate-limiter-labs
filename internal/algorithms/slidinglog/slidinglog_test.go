package slidinglog_test

import (
	"sync"
	"testing"
	"time"

	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/slidinglog"
)

func TestSlidingLog_Allow(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		calls     int
		wantAllow int
	}{
		{"happy path below limit", 10, 5, 5},
		{"exact limit boundary", 5, 5, 5},
		{"over limit denied", 5, 6, 5},
		{"single request limit", 1, 3, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rl := slidinglog.New(tc.limit, 60)
			allowed := 0
			for i := 0; i < tc.calls; i++ {
				if rl.Allow() {
					allowed++
				}
			}
			if allowed != tc.wantAllow {
				t.Errorf("allowed = %d, want %d", allowed, tc.wantAllow)
			}
		})
	}
}

func TestSlidingLog_AllowN(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		n     int
		want  bool
	}{
		{"AllowN within limit", 10, 5, true},
		{"AllowN equal to limit", 10, 10, true},
		{"AllowN over limit", 10, 11, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rl := slidinglog.New(tc.limit, 60)
			got := rl.AllowN(tc.n)
			if got != tc.want {
				t.Errorf("AllowN(%d) = %v, want %v", tc.n, got, tc.want)
			}
		})
	}
}

func TestSlidingLog_Reset(t *testing.T) {
	rl := slidinglog.New(3, 60)
	for i := 0; i < 3; i++ {
		rl.Allow()
	}
	if rl.Allow() {
		t.Fatal("expected rejection after exhausting limit")
	}
	rl.Reset()
	if !rl.Allow() {
		t.Fatal("expected allow after reset")
	}
}

func TestSlidingLog_WindowSlide(t *testing.T) {
	rl := slidinglog.New(2, 1) // 2 req per 1 second
	rl.Allow()
	rl.Allow()
	if rl.Allow() {
		t.Fatal("expected rejection at limit")
	}
	time.Sleep(1100 * time.Millisecond) // slide window past first requests
	if !rl.Allow() {
		t.Fatal("expected allow after window slide")
	}
}

func TestSlidingLog_NoBoundaryBurst(t *testing.T) {
	// The key advantage over fixed window: no 2x burst at boundary
	rl := slidinglog.New(5, 1)
	// fill window
	for i := 0; i < 5; i++ {
		if !rl.Allow() {
			t.Fatal("expected initial requests to be allowed")
		}
	}
	// even right at boundary, old requests still count until they expire
	if rl.Allow() {
		t.Fatal("expected rejection — sliding window still counts prior requests")
	}
}

func TestSlidingLog_Concurrent(t *testing.T) {
	t.Parallel()
	limit := 500
	rl := slidinglog.New(limit, 60)

	var (
		wg      sync.WaitGroup
		allowed int64
		mu      sync.Mutex
	)

	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := 0
			for i := 0; i < 20; i++ {
				if rl.Allow() {
					local++
				}
			}
			mu.Lock()
			allowed += int64(local)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if allowed > int64(limit) {
		t.Errorf("concurrent allowed %d, limit is %d", allowed, limit)
	}
}

func TestSlidingLog_Remaining(t *testing.T) {
	rl := slidinglog.New(5, 60)
	if rem := rl.Remaining(); rem != 5 {
		t.Errorf("empty log remaining = %d, want 5", rem)
	}
	rl.Allow()
	rl.Allow()
	if rem := rl.Remaining(); rem != 3 {
		t.Errorf("after 2 allows remaining = %d, want 3", rem)
	}
	rl.Reset()
	if rem := rl.Remaining(); rem != 5 {
		t.Errorf("after reset remaining = %d, want 5", rem)
	}
}
