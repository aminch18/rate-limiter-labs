---
name: go-algorithm
description: >
  Use when implementing a new rate limiting algorithm in this repo.
  Triggers on: "implement X algorithm", "add Y limiter", "create new algorithm".
allowed-tools: Read, Write, Edit, Bash(go:*), Bash(gofmt:*), Bash(git:*)
---

## When invoked

You are adding a new rate limiting algorithm to rate-limiter-labs.

## Checklist

1. Create `internal/algorithms/<name>/` directory
2. Implement `<name>.go` — must satisfy the `RateLimiter` interface in `ratelimiter.go`
3. Constructor signature: `func New<Name>(<params>) RateLimiter`
4. Write `<name>_test.go` with table-driven tests covering:
   - Allow() happy path
   - Allow() at exact limit boundary
   - Allow() over limit
   - AllowN(n)
   - Reset()
   - Concurrent access (t.Parallel + goroutines)
5. Add a `Benchmark<Name>` function to `benchmarks/bench_test.go`
6. Add a section to README.md explaining the algorithm and its trade-offs
7. Run: `go test ./... -race` — must be green before committing

## Style rules

- No global variables
- Mutex or atomic — no channels for simple counters
- Comments on exported types and functions (godoc style)
- Error handling: return bool, never panic
