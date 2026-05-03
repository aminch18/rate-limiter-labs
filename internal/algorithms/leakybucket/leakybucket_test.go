package leakybucket_test

import (
	"sync"
	"testing"
	"time"

	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/leakybucket"
)

func TestLeakyBucket_Allow(t *testing.T) {
	tests := []struct {
		name      string
		capacity  int
		calls     int
		wantAllow int
	}{
		{"happy path below capacity", 10, 5, 5},
		{"exact capacity boundary", 5, 5, 5},
		{"over capacity dropped", 5, 6, 5},
		{"single slot bucket", 1, 2, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rl := leakybucket.New(tc.capacity, 0) // rate=0 so queue never drains
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

func TestLeakyBucket_AllowN(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		n        int
		want     bool
	}{
		{"AllowN within capacity", 10, 5, true},
		{"AllowN equal to capacity", 10, 10, true},
		{"AllowN over capacity", 10, 11, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rl := leakybucket.New(tc.capacity, 0)
			got := rl.AllowN(tc.n)
			if got != tc.want {
				t.Errorf("AllowN(%d) = %v, want %v", tc.n, got, tc.want)
			}
		})
	}
}

func TestLeakyBucket_Reset(t *testing.T) {
	rl := leakybucket.New(3, 0)
	for i := 0; i < 3; i++ {
		rl.Allow()
	}
	if rl.Allow() {
		t.Fatal("expected drop after filling queue")
	}
	rl.Reset()
	if !rl.Allow() {
		t.Fatal("expected allow after reset")
	}
}

func TestLeakyBucket_Drain(t *testing.T) {
	rl := leakybucket.New(5, 1000) // drains 1000 req/sec
	for i := 0; i < 5; i++ {
		rl.Allow()
	}
	if rl.Allow() {
		t.Fatal("queue should be full")
	}
	time.Sleep(5 * time.Millisecond) // should drain ~5 slots
	if !rl.Allow() {
		t.Fatal("expected queue slot after drain interval")
	}
}

func TestLeakyBucket_Concurrent(t *testing.T) {
	t.Parallel()
	capacity := 500
	rl := leakybucket.New(capacity, 0)

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

	if allowed > int64(capacity) {
		t.Errorf("concurrent allowed %d, capacity is %d", allowed, capacity)
	}
}

func TestLeakyBucket_Remaining(t *testing.T) {
	rl := leakybucket.New(5, 0) // rate=0 so no drain during test
	if rem := rl.Remaining(); rem != 5 {
		t.Errorf("empty queue remaining = %d, want 5", rem)
	}
	rl.Allow()
	rl.Allow()
	if rem := rl.Remaining(); rem != 3 {
		t.Errorf("after 2 enqueued remaining = %d, want 3", rem)
	}
	rl.Reset()
	if rem := rl.Remaining(); rem != 5 {
		t.Errorf("after reset remaining = %d, want 5", rem)
	}
}
