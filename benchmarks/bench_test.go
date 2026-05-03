// Package benchmarks runs all rate limiting algorithms under identical load profiles
// and produces a comparative table of throughput, latency, and allocations.
package benchmarks

import (
	"runtime"
	"sync"
	"testing"

	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/fixedwindow"
	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/leakybucket"
	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/slidingcounter"
	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/slidinglog"
	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/tokenbucket"
)

const (
	benchCapacity = 1000
	benchRate     = 100.0    // tokens/sec for token bucket
	benchWindow   = 1        // second for window-based limiters
	benchLimit    = 1000     // requests per window (counter-based algorithms)
	benchLogLimit = 1 << 30  // effectively unlimited — forces SlidingLog to always accept,
	// so we benchmark the append+purge path, not the cheap early-return rejection path.
	// Using benchLimit would fill the window in microseconds; every subsequent call would
	// reject at the len-check before any append, making SlidingLog look as fast as FixedWindow
	// and hiding its true O(n) scan cost.
	//
	// Note on B/op: after ~1 s of warmup the backing []time.Time reaches steady-state
	// capacity (purge evicts as fast as Allow appends), so no further heap allocations
	// occur and -benchmem reports 0 B/op at steady state. The real cost is peak HELD
	// memory: requests_in_window × 24 bytes per client.
)

// --- Token Bucket ---

func BenchmarkTokenBucket_Steady(b *testing.B) {
	rl := tokenbucket.New(benchCapacity, benchRate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow()
	}
}

func BenchmarkTokenBucket_Burst(b *testing.B) {
	rl := tokenbucket.New(benchCapacity, benchRate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.AllowN(10)
	}
}

func BenchmarkTokenBucket_Concurrent(b *testing.B) {
	rl := tokenbucket.New(benchCapacity, benchRate)
	procs := runtime.GOMAXPROCS(0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		_ = procs
		for pb.Next() {
			rl.Allow()
		}
	})
}

// --- Fixed Window ---

func BenchmarkFixedWindow_Steady(b *testing.B) {
	rl := fixedwindow.New(benchLimit, benchWindow)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow()
	}
}

func BenchmarkFixedWindow_Burst(b *testing.B) {
	rl := fixedwindow.New(benchLimit, benchWindow)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.AllowN(10)
	}
}

func BenchmarkFixedWindow_Concurrent(b *testing.B) {
	rl := fixedwindow.New(benchLimit, benchWindow)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rl.Allow()
		}
	})
}

// --- Leaky Bucket ---

func BenchmarkLeakyBucket_Steady(b *testing.B) {
	rl := leakybucket.New(benchCapacity, benchRate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow()
	}
}

func BenchmarkLeakyBucket_Burst(b *testing.B) {
	rl := leakybucket.New(benchCapacity, benchRate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.AllowN(10)
	}
}

func BenchmarkLeakyBucket_Concurrent(b *testing.B) {
	rl := leakybucket.New(benchCapacity, benchRate)
	var wg sync.WaitGroup
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		wg.Add(1)
		defer wg.Done()
		for pb.Next() {
			rl.Allow()
		}
	})
	wg.Wait()
}

// --- Sliding Window Log ---
// Uses benchLogLimit so every call takes the accept path (log never full).
// With no pre-allocated capacity in the constructor, slice growth allocations
// ARE visible to -benchmem — revealing the true O(n × 24 bytes) memory cost.

func BenchmarkSlidingLog_Steady(b *testing.B) {
	rl := slidinglog.New(benchLogLimit, benchWindow)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow()
	}
}

func BenchmarkSlidingLog_Burst(b *testing.B) {
	rl := slidinglog.New(benchLogLimit, benchWindow)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.AllowN(10)
	}
}

func BenchmarkSlidingLog_Concurrent(b *testing.B) {
	rl := slidinglog.New(benchLogLimit, benchWindow)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rl.Allow()
		}
	})
}

// --- Sliding Window Counter ---

func BenchmarkSlidingCounter_Steady(b *testing.B) {
	rl := slidingcounter.New(benchLimit, benchWindow)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow()
	}
}

func BenchmarkSlidingCounter_Burst(b *testing.B) {
	rl := slidingcounter.New(benchLimit, benchWindow)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.AllowN(10)
	}
}

func BenchmarkSlidingCounter_Concurrent(b *testing.B) {
	rl := slidingcounter.New(benchLimit, benchWindow)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rl.Allow()
		}
	})
}
