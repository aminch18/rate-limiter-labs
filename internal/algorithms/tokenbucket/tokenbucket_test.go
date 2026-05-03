package tokenbucket_test

import (
	"sync"
	"testing"
	"time"

	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/tokenbucket"
)

func TestTokenBucket_Allow(t *testing.T) {
	tests := []struct {
		name      string
		capacity  int
		rate      float64
		calls     int
		wantAllow int // how many of the first 'calls' should be allowed
	}{
		{"happy path below capacity", 10, 1, 5, 5},
		{"exact capacity boundary", 5, 1, 5, 5},
		{"over limit denied", 5, 1, 6, 5},
		{"single token bucket", 1, 1, 2, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rl := tokenbucket.New(tc.capacity, tc.rate)
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

func TestTokenBucket_AllowN(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		rate     float64
		n        int
		want     bool
	}{
		{"AllowN within capacity", 10, 1, 5, true},
		{"AllowN equal to capacity", 10, 1, 10, true},
		{"AllowN over capacity", 10, 1, 11, false},
		{"AllowN zero", 10, 1, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rl := tokenbucket.New(tc.capacity, tc.rate)
			got := rl.AllowN(tc.n)
			if got != tc.want {
				t.Errorf("AllowN(%d) = %v, want %v", tc.n, got, tc.want)
			}
		})
	}
}

func TestTokenBucket_Reset(t *testing.T) {
	rl := tokenbucket.New(3, 1)
	// drain
	for i := 0; i < 3; i++ {
		rl.Allow()
	}
	if rl.Allow() {
		t.Fatal("expected rejection after draining, got allow")
	}
	rl.Reset()
	if !rl.Allow() {
		t.Fatal("expected allow after reset, got rejection")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	rl := tokenbucket.New(10, 1000) // 1000 tokens/sec
	// drain completely
	for i := 0; i < 10; i++ {
		rl.Allow()
	}
	if rl.Allow() {
		t.Fatal("expected empty bucket")
	}
	// wait enough for at least 1 token to refill
	time.Sleep(2 * time.Millisecond)
	if !rl.Allow() {
		t.Fatal("expected at least one token after waiting for refill")
	}
}

func TestTokenBucket_Concurrent(t *testing.T) {
	t.Parallel()
	capacity := 1000
	rl := tokenbucket.New(capacity, 0) // no refill — finite tokens only

	var (
		wg      sync.WaitGroup
		allowed int64
		mu      sync.Mutex
	)

	goroutines := 50
	callsEach := 30

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := 0
			for i := 0; i < callsEach; i++ {
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
		t.Errorf("concurrent allowed %d requests, capacity is %d — data race or over-admission", allowed, capacity)
	}
}

func TestTokenBucket_Remaining(t *testing.T) {
	rl := tokenbucket.New(10, 0) // rate=0 so no refill during test
	if rem := rl.Remaining(); rem != 10 {
		t.Errorf("fresh bucket remaining = %d, want 10", rem)
	}
	rl.Allow()
	rl.Allow()
	if rem := rl.Remaining(); rem != 8 {
		t.Errorf("after 2 allows remaining = %d, want 8", rem)
	}
	rl.Reset()
	if rem := rl.Remaining(); rem != 10 {
		t.Errorf("after reset remaining = %d, want 10", rem)
	}
}
