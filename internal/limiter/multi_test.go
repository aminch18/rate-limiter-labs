package limiter_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms"
	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/fixedwindow"
	"github.com/tu-usuario/rate-limiter-labs/internal/limiter"
)

func fixedFactory(limit int) func() algorithms.RateLimiter {
	return func() algorithms.RateLimiter {
		return fixedwindow.New(limit, 60)
	}
}

func TestMulti_IsolatesKeys(t *testing.T) {
	m := limiter.NewMulti[string](fixedFactory(3))

	for i := 0; i < 3; i++ {
		if !m.Allow("a") {
			t.Fatalf("allow call %d for key 'a' should succeed", i+1)
		}
	}
	if m.Allow("a") {
		t.Fatal("key 'a' should be exhausted after 3 allows")
	}
	// key "b" is independent — full quota
	if !m.Allow("b") {
		t.Fatal("key 'b' should have full quota unaffected by 'a'")
	}
}

func TestMulti_Remaining(t *testing.T) {
	m := limiter.NewMulti[string](fixedFactory(5))

	// Key not yet seen: -1 signals "no limiter created yet, no side effect on read".
	if rem := m.Remaining("x"); rem != -1 {
		t.Errorf("unseen key remaining = %d, want -1", rem)
	}
	if m.Len() != 0 {
		t.Error("Remaining on unseen key must not create a limiter entry")
	}

	m.Allow("x") // creates limiter, consumes 1
	m.Allow("x") // consumes 2nd
	if rem := m.Remaining("x"); rem != 3 {
		t.Errorf("after 2 allows remaining = %d, want 3", rem)
	}
	m.Reset("x")
	if rem := m.Remaining("x"); rem != 5 {
		t.Errorf("after reset remaining = %d, want 5", rem)
	}
}

func TestMulti_Len(t *testing.T) {
	m := limiter.NewMulti[string](fixedFactory(10))
	m.Allow("a")
	m.Allow("b")
	m.Allow("a") // duplicate key — must not create a second entry
	if n := m.Len(); n != 2 {
		t.Errorf("Len() = %d, want 2", n)
	}
}

func TestMulti_Concurrent(t *testing.T) {
	t.Parallel()
	const (
		keyCount   = 10
		perKeyLimit = 100
		goroutines  = keyCount
		callsEach   = 200 // 2× limit — half should be denied
	)

	m := limiter.NewMulti[int](fixedFactory(perKeyLimit))

	var wg sync.WaitGroup
	var totalAllowed atomic.Int64

	for key := 0; key < goroutines; key++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for i := 0; i < callsEach; i++ {
				if m.Allow(k) {
					totalAllowed.Add(1)
				}
			}
		}(key)
	}
	wg.Wait()

	maxAllowed := int64(keyCount * perKeyLimit)
	if got := totalAllowed.Load(); got > maxAllowed {
		t.Errorf("total allowed %d exceeds %d keys × limit %d = %d",
			got, keyCount, perKeyLimit, maxAllowed)
	}
}
