# PRD — Rate Limiter Labs

**Version:** 0.1.0-draft  
**Author:** tu-usuario  
**Date:** 2026-05-02  
**Status:** Draft  
**Repo:** github.com/tu-usuario/rate-limiter-labs

---

## 1. Problem Statement

Rate limiting is one of those fundamental system design concepts that every senior engineer talks about in interviews and architecture reviews, but almost nobody has actually *implemented* from scratch. The typical developer knows that rate limiters exist, knows vaguely that there are "algorithms", and reaches for a library without understanding the trade-offs.

This project exists to close that gap — not through documentation, but through code you write and benchmarks you run yourself.

**What's missing in the ecosystem:**

- No single Go repo that implements *all* the major algorithms with a consistent interface
- No comparative benchmark harness that makes trade-offs visible as numbers
- No learning-oriented codebase that explains *why* each algorithm exists, not just *how* it works

---

## 2. Goal

Build a Go benchmarking lab that implements every major rate limiting algorithm under a unified interface, with production-quality tests, comparative benchmarks, and documentation that explains the trade-offs of each approach.

**Primary goal:** Learn. Go deeper on Go concurrency, algorithmic trade-offs, and benchmark methodology.  
**Secondary goal:** Produce a reference implementation good enough to link from real conversations.

---

## 3. Non-goals

- This is NOT a production library. No public API stability guarantees.
- This is NOT a framework or middleware. No HTTP handlers, no gRPC interceptors.
- This is NOT a distributed rate limiter. No Redis, no Lua scripts, no cluster coordination — yet.

---

## 4. Algorithms to Implement

Each algorithm lives in its own package under `internal/algorithms/`. All implement the same `RateLimiter` interface.

### 4.1 Token Bucket
**The reference implementation.** Most systems mean "token bucket" when they say "rate limiter."

- A bucket holds up to `capacity` tokens
- Tokens refill at a constant rate (`rate` tokens/second)
- Each request consumes one token; if the bucket is empty, the request is rejected (or waits)
- **Strength:** Handles bursts naturally up to `capacity`
- **Weakness:** Slightly complex refill math under high concurrency

### 4.2 Leaky Bucket
The "output shaping" algorithm. Requests enter a queue (the bucket) and are processed at a fixed rate, like water leaking from a hole.

- Requests queue up; if the queue is full, they are dropped
- Processing rate is constant regardless of input spikes
- **Strength:** Smooth, predictable output rate
- **Weakness:** Adds latency; queue adds memory pressure; bursts are completely flattened

### 4.3 Fixed Window Counter
The simplest algorithm. Divide time into fixed windows (e.g., 1-second buckets), count requests per window.

- Reset the counter at the start of each window
- Reject requests once the counter exceeds the limit
- **Strength:** O(1) memory, trivially simple
- **Weakness:** The "boundary burst" problem — a client can send `2×limit` requests by straddling two windows

### 4.4 Sliding Window Log
The accurate fix for the Fixed Window boundary problem. Store a timestamp for every request in a sorted log.

- On each request: purge timestamps older than `window` duration, count remaining, reject if over limit
- **Strength:** Precise — no boundary bursts
- **Weakness:** O(n) memory per client where n = requests per window; expensive under high load

### 4.5 Sliding Window Counter
The pragmatic hybrid. Combines Fixed Window simplicity with an approximation of Sliding Window accuracy.

- Keep two fixed window counters: current window and previous window
- Estimate current rate using a weighted blend:  
  `estimated = prev_count × (1 - elapsed/window) + curr_count`
- **Strength:** O(1) memory, good accuracy, no boundary bursts in practice
- **Weakness:** Approximation — not mathematically exact

---

## 5. Core Interface

```go
// RateLimiter is the single interface all algorithms implement.
// All implementations must be safe for concurrent use.
type RateLimiter interface {
    // Allow returns true if the request is permitted under the current rate limit.
    // It must be safe to call from multiple goroutines simultaneously.
    Allow() bool

    // AllowN returns true if n requests are permitted.
    // Useful for batch operations.
    AllowN(n int) bool

    // Reset resets the limiter to its initial state.
    // Useful in tests and benchmarks between runs.
    Reset()
}
```

Every algorithm also exposes a constructor:

```go
// Example: Token Bucket
func NewTokenBucket(capacity int, ratePerSec float64) RateLimiter
```

---

## 6. Project Structure

```
rate-limiter-labs/
├── CLAUDE.md                        # Claude Code contract (conventions, non-goals, commands)
├── PRD.md                           # This document
├── README.md                        # Overview + benchmark results table
├── go.mod
├── go.sum
│
├── internal/
│   └── algorithms/
│       ├── ratelimiter.go           # RateLimiter interface + shared types
│       ├── tokenbucket/
│       │   ├── tokenbucket.go
│       │   └── tokenbucket_test.go
│       ├── leakybucket/
│       │   ├── leakybucket.go
│       │   └── leakybucket_test.go
│       ├── fixedwindow/
│       │   ├── fixedwindow.go
│       │   └── fixedwindow_test.go
│       ├── slidinglog/
│       │   ├── slidinglog.go
│       │   └── slidinglog_test.go
│       └── slidingcounter/
│           ├── slidingcounter.go
│           └── slidingcounter_test.go
│
├── benchmarks/
│   ├── bench_test.go                # go test -bench — runs all algorithms under the same load profiles
│   └── results/
│       └── README.md                # Benchmark results table (committed after each run)
│
└── .claude/
    ├── settings.json                # Hooks: gofmt, go vet, go test -race
    └── skills/
        └── go-algorithm/
            └── SKILL.md             # Claude skill: how to add a new algorithm to this repo
```

---

## 7. Testing Requirements

Every algorithm package must have:

**Unit tests** (`*_test.go` alongside the implementation):
- Table-driven (`[]struct{ name, input, expected }`)
- At minimum: happy path, exact-limit boundary, over-limit, `AllowN`, `Reset`
- Must pass `go test -race ./...` — no data races allowed

**Benchmark** (in `benchmarks/bench_test.go`):
- `BenchmarkXxx` function per algorithm
- Always run with `-benchmem` to expose allocations
- Three load profiles: steady (100 req/s), burst (1000 req/s × 10ms), concurrent (GOMAXPROCS goroutines hammering simultaneously)

---

## 8. Benchmark Harness

The file `benchmarks/bench_test.go` runs every algorithm under identical conditions and prints a comparison table:

```
Algorithm             ops/sec       ns/op     allocs/op     B/op
─────────────────────────────────────────────────────────────────
TokenBucket           12,450,000    80.3       0            0
LeakyBucket            8,200,000   121.9       1           24
FixedWindow           18,900,000    52.8       0            0
SlidingWindowLog         980,000  1021.4       3           96
SlidingWindowCounter  17,100,000    58.4       0            0
```

Results are committed to `benchmarks/results/` after every implementation milestone.

---

## 9. CLAUDE.md Contract

The `CLAUDE.md` at the repo root is the binding contract for Claude Code sessions. It must contain:

```markdown
# Rate Limiter Labs — Claude Code Contract

## Module
github.com/tu-usuario/rate-limiter-labs

## Go version
1.22+

## Commands
- Test:      go test ./... -race
- Bench:     go test ./benchmarks/ -bench=. -benchmem -count=3
- Lint:      go vet ./...
- Format:    gofmt -w .

## Non-negotiable rules
- Every algorithm MUST implement internal/algorithms/ratelimiter.go#RateLimiter
- Every algorithm package lives under internal/algorithms/<name>/
- Every implementation file has a companion _test.go
- Tests are table-driven
- No global state in algorithm implementations
- No external dependencies (standard library only)
- go test -race must pass before any commit

## What NOT to do
- Do not create god packages
- Do not add HTTP handlers or middleware
- Do not add Redis or any network dependency
- Do not merge to main without a passing test suite
```

---

## 10. Gentle-AI Integration

This project runs on top of the Gentle-AI ecosystem. Before the first Claude Code session:

```bash
# Install Gentle-AI
brew tap Gentleman-Programming/homebrew-tap && brew install gentle-ai
# Or: go install github.com/gentleman-programming/gentle-ai/cmd/gentle-ai@latest

# Dry-run first — see exactly what it will touch
gentle-ai install --dry-run --agent claude-code --preset full-gentleman

# Apply
gentle-ai install --agent claude-code --preset full-gentleman
```

This injects into Claude Code:
- **Engram** — persistent memory across sessions (remembers decisions, bugs, conventions)
- **SDD** — Spec-Driven Development (Claude plans before it codes; no surprise rewrites)
- **Context7 MCP** — live Go stdlib and library docs during sessions
- **Gentleman Persona** — mentor mode: Claude explains trade-offs, doesn't just dump code
- **Permissions** — security-first defaults (blocks `.env` access, etc.)

After `gentle-ai install`, add the project-specific skill on top:

```bash
mkdir -p .claude/skills/go-algorithm
# Write .claude/skills/go-algorithm/SKILL.md (see Section 11)
```

---

## 11. Project Skill: go-algorithm

`.claude/skills/go-algorithm/SKILL.md`:

```markdown
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
```

---

## 12. Claude Code Hooks

`.claude/settings.json`:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "gofmt -w $(echo $CLAUDE_FILE_PATHS) && go vet ./..."
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "go test ./... -race -count=1 -timeout=60s"
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "[ \"$(git branch --show-current)\" != \"main\" ] || { echo '{\"block\": true, \"message\": \"Cannot edit on main. Create a feature branch first.\"}' >&2; exit 2; }"
          }
        ]
      }
    ]
  }
}
```

---

## 13. GitHub MCP — Automated PR Workflow

After connecting the GitHub MCP server (Step 4 of the setup plan), each algorithm follows this exact flow:

```
claude session:
  1. Implement algorithm + tests (via go-algorithm skill)
  2. "Create branch feat/sliding-window-log, commit with conventional message,
      push to GitHub, open PR with algorithm explanation and benchmark numbers"
  3. Claude uses GitHub MCP tools to do all of this without leaving the terminal
```

---

## 14. Implementation Order & Milestones

| Milestone | Branch | Goal |
|---|---|---|
| M0 — Scaffolding | `feat/scaffold` | Interface, project structure, CLAUDE.md, hooks, skills, empty benchmark harness |
| M1 — Token Bucket | `feat/token-bucket` | Reference implementation + tests + benchmark |
| M2 — Fixed Window | `feat/fixed-window` | Simplest counter algorithm |
| M3 — Leaky Bucket | `feat/leaky-bucket` | Output shaping variant |
| M4 — Sliding Log | `feat/sliding-log` | Precise but expensive |
| M5 — Sliding Counter | `feat/sliding-counter` | Pragmatic hybrid |
| M6 — Harness | `feat/benchmark-harness` | Full comparative table, results committed |
| M7 — Gateway | `feat/gateway` | HTTP gateway + load generator, see Section 15 |

Each milestone = one Claude Code session = one PR via GitHub MCP.

---

## 15. Live Gateway (v2 — Option B)

The algorithm lab proves correctness and measures raw throughput. The gateway proves *behavior* — how each algorithm feels under real traffic patterns.

### What it is

A single Go binary (`cmd/gateway/main.go`) that exposes one HTTP endpoint per algorithm, each with its own limiter instance. A companion load generator (`cmd/loadgen/main.go`) hammers those endpoints with three traffic patterns and prints a side-by-side comparison table.

### Endpoints

| Route | Algorithm | Config |
|---|---|---|
| `GET /token-bucket` | Token Bucket | capacity=20, rate=10/sec |
| `GET /fixed-window` | Fixed Window | limit=10/sec |
| `GET /leaky-bucket` | Leaky Bucket | capacity=20, rate=10/sec |
| `GET /sliding-log` | Sliding Log | limit=10/sec |
| `GET /sliding-counter` | Sliding Counter | limit=10/sec |

Every response includes the headers:
- `X-RateLimit-Remaining` — slots left in current window
- `X-RateLimit-Algorithm` — which algorithm handled the request

### Traffic patterns (loadgen)

1. **Steady** — 5 req/sec for 5 seconds (well below limit, all should pass)
2. **Burst** — 30 req fired instantly, then silence (exposes boundary behavior)
3. **Sustained over-limit** — 20 req/sec for 5 seconds (2× limit, shows throttle %)

### Output

```
Pattern: burst (30 req instant)
────────────────────────────────────────────────────────
Algorithm            Allowed   Denied   Allow%   Notes
────────────────────────────────────────────────────────
TokenBucket              20       10    66.7%   burst absorbed to capacity
FixedWindow              10       20    33.3%   hard cutoff at window limit
LeakyBucket              20       10    66.7%   queue absorbed burst
SlidingLog               10       20    33.3%   precise, same as fixed here
SlidingCounter           10       20    33.3%   approx same as fixed here
────────────────────────────────────────────────────────
```

### Project structure additions

```
rate-limiter-labs/
├── cmd/
│   ├── gateway/
│   │   └── main.go      # HTTP server
│   └── loadgen/
│       └── main.go      # Traffic simulator + results table
```

### Running

```bash
# Terminal 1
go run ./cmd/gateway

# Terminal 2
go run ./cmd/loadgen
```

No external dependencies — stdlib `net/http` only.

---

## 16. Open Questions

- **Distributed phase (v3)?** Redis-backed sliding window counter
- **Generics?** Go 1.21+ generics for `AllowN[T numeric]` — evaluate after M1
- **Key-based limiting?** `Allow(key string)` for per-IP / per-user scoping
