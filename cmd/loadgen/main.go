// Loadgen fires three traffic patterns against the gateway and prints a
// side-by-side comparison table showing how each algorithm behaves differently.
//
// Patterns:
//
//	steady   — 20 req @ 5/sec (well below limit — all should pass)
//	burst    — 30 requests fired simultaneously (exposes capacity differences)
//	overload — 60 req @ 20/sec (2× limit — shows throttle percentage)
//
// Run: go run ./cmd/loadgen
//
//	go run ./cmd/loadgen -addr host:port
package main

import (
	"flag"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var baseURL string

var endpoints = []struct {
	key  string
	name string
}{
	{"token-bucket", "TokenBucket"},
	{"fixed-window", "FixedWindow"},
	{"leaky-bucket", "LeakyBucket"},
	{"sliding-log", "SlidingLog"},
	{"sliding-counter", "SlidingCounter"},
}

type result struct {
	allowed int64
	denied  int64
	notes   string
}

// sendN fires n HTTP GET requests to url, spacing them interval apart.
// interval == 0 means fire all requests concurrently (burst mode).
// Uses a time.Ticker instead of time.Sleep for consistent inter-request spacing:
// the ticker advances by exactly interval regardless of how long each request takes,
// so the measured rate stays stable across slow and fast machines.
// Uses atomics on counters so goroutines never block each other on updates.
func sendN(url string, n int, interval time.Duration) result {
	client := &http.Client{Timeout: 2 * time.Second}
	var wg sync.WaitGroup
	var r result

	fire := func() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(url)
			if err != nil {
				atomic.AddInt64(&r.denied, 1)
				return
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				atomic.AddInt64(&r.allowed, 1)
			} else {
				atomic.AddInt64(&r.denied, 1)
			}
		}()
	}

	if interval <= 0 {
		// Burst: launch all goroutines immediately.
		for i := 0; i < n; i++ {
			fire()
		}
	} else {
		// Paced: use a ticker so the rate is wall-clock accurate.
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for i := 0; i < n; i++ {
			<-ticker.C
			fire()
		}
	}
	wg.Wait()
	return r
}

type pattern struct {
	name    string
	desc    string
	runFunc func(url string) result
	noteFor func(key string) string // returns a note for each algorithm key, or ""
}

var burstNotes = map[string]string{
	"token-bucket":    "absorbs burst up to capacity=20",
	"fixed-window":    "hard cutoff at limit=10",
	"leaky-bucket":    "absorbs burst up to capacity=20",
	"sliding-log":     "exact sliding window, limit=10",
	"sliding-counter": "approx sliding window, limit=10",
}

var overloadNotes = map[string]string{
	"token-bucket":    "tokens refill during sustained load",
	"fixed-window":    "hard reset each window",
	"leaky-bucket":    "drain rate smooths input",
	"sliding-log":     "exact, no approximation",
	"sliding-counter": "weighted blend, O(1) memory",
}

var patterns = []pattern{
	{
		name: "steady",
		desc: "20 req @ 5/sec (below limit)",
		runFunc: func(url string) result {
			return sendN(url, 20, 200*time.Millisecond)
		},
		noteFor: func(string) string { return "" },
	},
	{
		name: "burst",
		desc: "30 req all at once",
		runFunc: func(url string) result {
			return sendN(url, 30, 0)
		},
		noteFor: func(key string) string { return burstNotes[key] },
	},
	{
		name: "overload",
		desc: "60 req @ 20/sec (2× limit)",
		runFunc: func(url string) result {
			return sendN(url, 60, 50*time.Millisecond)
		},
		noteFor: func(key string) string { return overloadNotes[key] },
	},
}

func printTable(patternName, desc string, results map[string]result) {
	fmt.Printf("\nPattern: %s — %s\n", patternName, desc)
	fmt.Printf("%-20s %8s %8s %8s   %s\n", "Algorithm", "Allowed", "Denied", "Allow%", "Notes")
	fmt.Println("──────────────────────────────────────────────────────────────────")
	for _, ep := range endpoints {
		r := results[ep.key]
		total := r.allowed + r.denied
		pct := 0.0
		if total > 0 {
			pct = float64(r.allowed) / float64(total) * 100
		}
		notes := ""
		if r.notes != "" {
			notes = "← " + r.notes
		}
		fmt.Printf("%-20s %8d %8d %7.1f%%   %s\n", ep.name, r.allowed, r.denied, pct, notes)
	}
}

func main() {
	addr := flag.String("addr", "localhost:8080", "gateway address")
	flag.Parse()
	baseURL = "http://" + *addr

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Printf("gateway not reachable at %s\nStart it with: go run ./cmd/gateway\n", baseURL)
		return
	}
	resp.Body.Close()

	fmt.Println("Rate Limiter Labs — Live Comparison")
	fmt.Printf("Gateway: %s\n", baseURL)
	fmt.Println("Config:  capacity/limit=20 (token/leaky) | 10 req/sec (window-based)")

	for _, p := range patterns {
		results := make(map[string]result, len(endpoints))
		// Run each algorithm sequentially so window limiters start fresh.
		// The 1.1s sleep ensures window-based limiters have rolled over before the next algorithm runs.
		for _, ep := range endpoints {
			r := p.runFunc(baseURL + "/" + ep.key)
			r.notes = p.noteFor(ep.key)
			results[ep.key] = r
			time.Sleep(1100 * time.Millisecond)
		}
		printTable(p.name, p.desc, results)
	}

	fmt.Println()
}
