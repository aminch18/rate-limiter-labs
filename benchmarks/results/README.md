# Benchmark Results

**Machine:** Windows 11, 13th Gen Intel Core i7-13700F, 24 logical CPUs  
**Go:** 1.26.2 windows/amd64  
**Command:** `go test -bench="." -benchmem -count=3 ./benchmarks/`  
**Date:** 2026-05-02

---

## Results (median of 3 runs)

```
Benchmark                          ns/op    B/op   allocs/op
──────────────────────────────────────────────────────────────
TokenBucket/Steady                 13.71      0        0
TokenBucket/Burst                  13.88      0        0
TokenBucket/Concurrent             40.15      0        0

FixedWindow/Steady                 11.87      0        0
FixedWindow/Burst                  19.96      0        0
FixedWindow/Concurrent             76.60      0        0

LeakyBucket/Steady                 23.95      0        0
LeakyBucket/Burst                  23.43      0        0
LeakyBucket/Concurrent             89.94      0        0

SlidingLog/Steady                  40.24      0        0    ← 3–4× slower; memory cost is peak held, not per-op
SlidingLog/Burst                  206.6       0        0    ← 10 appends + purge scan each call
SlidingLog/Concurrent             106.8       0        0

SlidingCounter/Steady              18.61      0        0
SlidingCounter/Burst               18.05      0        0
SlidingCounter/Concurrent          84.67      0        0
```

---

## Reading the numbers correctly

### Why SlidingLog shows B/op = 0 despite O(n) memory

`-benchmem` reports `TotalAlloc / b.N` (total bytes heap-allocated, amortised per operation).

SlidingLog **does** allocate heavily — but only during the first ~1 second of warmup,
while the backing `[]time.Time` grows from 0 → steady-state capacity via Go's standard
doubling strategy (log₂(n) actual allocations for n appended entries).

Once the slice reaches its natural working size — roughly
`requests_in_window × 24 bytes` of capacity — `purge()` evicts old entries as fast as
`AllowN` appends new ones. At that steady state, `append` finds cap > len and writes
in-place: **zero new allocations per call**.

Because the benchmark runs for millions of iterations after that warmup, the allocation
cost is amortized across the full run and rounds to 0 B/op.

**The real cost is the memory held, not the per-op allocations:**

| Rate | Window | Peak memory per client |
|------|--------|------------------------|
| 1 000 req/s | 1 s | ~24 KB |
| 100 000 req/s | 1 s | ~2.4 MB |
| 1 000 req/s | 60 s | ~1.4 MB |

Compare with every other algorithm: they hold at most a handful of int/float/time
fields — **constant memory regardless of traffic**.

### Why SlidingLog/Burst is 5–15× slower than /Steady

Each `AllowN(10)` call:
1. Acquires the mutex
2. Calls `purge()` — scans from the front of the log (O(k) for k expired entries)
3. Appends **10** `time.Time` values instead of 1

Under the `benchLogLimit = 1<<30` constant (log never full) with a 1-second window,
the log grows to tens of millions of entries during the benchmark. The `purge` scan
dominates: it must walk further through the log for each call.

### Why allocs/op = 0 everywhere

Go's runtime only counts **heap escapes** — objects that cannot live on the goroutine
stack. All algorithm state (ints, floats, `time.Time`) lives in pre-allocated struct
fields. The SlidingLog slice header also lives on the struct; only the backing array
escapes, and as explained above, it stops growing at steady state.

### Concurrent performance

Under 24 goroutines, all algorithms converge toward similar numbers. Lock contention
on the shared `sync.Mutex` becomes the bottleneck — not algorithmic complexity.

In production, shard limiters by key (`internal/limiter/Multi[K]`) so each key
gets its own mutex and goroutines for different clients never contend.

---

## Key trade-offs

| Algorithm | Steady ns/op | Memory model | Notes |
|---|---|---|---|
| FixedWindow | 11.9 | O(1) — two ints | Boundary burst risk |
| SlidingCounter | 18.6 | O(1) — two ints + float blend | Approximate; eliminates burst |
| TokenBucket | 13.7 | O(1) — float counter + timestamp | Allows controlled burst |
| LeakyBucket | 24.0 | O(1) — int queue depth + timestamp | Smoothest output rate |
| SlidingLog | 40.2 | **O(n)** — n = requests in window | Exact; high memory at scale |

SlidingLog is 3–4× slower and the only algorithm whose **resident memory scales
linearly with traffic**. The trade-off: it is the only algorithm with mathematically
exact sliding window semantics — no approximation, no boundary bursts, no blending.

---

## Reproducing

```bash
go test -bench="." -benchmem -count=3 ./benchmarks/
```

> **Note on `-race`:** requires CGO (a C compiler) on Windows.
> On Linux/macOS run `go test ./... -race` to verify no data races.
