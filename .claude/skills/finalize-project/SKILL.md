---
name: finalize-project
description: >
  Use when completing and shipping the rate-limiter-labs project.
  Triggers on: "finalize", "finish the project", "ship it", "complete everything",
  "what's left", "close out the project", "ready to share".
allowed-tools: Read, Write, Edit, Bash(go:*), Bash(git:*), Bash(curl:*)
---

## Project state (as of last context)

### DONE
- [x] M0 Scaffolding (go.mod, CLAUDE.md, hooks, skills)
- [x] M1–M5 All 5 algorithms implemented + tests pass
- [x] Multi[K] key-based limiter (internal/limiter/multi.go)
- [x] M6 Benchmarks run, results in benchmarks/results/README.md
- [x] M7 Gateway (cmd/gateway/main.go) + Loadgen (cmd/loadgen/main.go) implemented
- [x] README.md in Spanish with quickstart and algorithm explanations

### NOT DONE — do these in order
- [ ] 1. Verify gateway + loadgen produce correct output (run the verify-gateway skill)
- [ ] 2. Fix any issues found in step 1
- [ ] 3. Fix known README inaccuracy: Leaky Bucket row says "O(n) cola" but the
          implementation is the METER variant (O(1) memory, no actual queue)
- [ ] 4. Commit all changes on a feature branch, then merge to main
- [ ] 5. (Optional) Add the Notes column to loadgen output table

## Exact completion checklist

### Step 1 — Verify gateway end-to-end

Run the `verify-gateway` skill. Expected outcome: burst pattern shows
TokenBucket/LeakyBucket allowing ~20 requests vs FixedWindow's ~10.

```bash
go run ./cmd/gateway &
sleep 2
go run ./cmd/loadgen
```

### Step 2 — Fix README.md Leaky Bucket row

File: `README.md`, the algorithms table.

Current (wrong):
```
| Leaky Bucket | O(n) cola | Aplana bursts | No | Output rate uniforme |
```

Correct:
```
| Leaky Bucket | O(1) | Aplana bursts | No | Output rate uniforme |
```

The implementation in `internal/algorithms/leakybucket/leakybucket.go` uses the
"as a meter" variant — it tracks queue depth as an integer counter, not an actual
queue of requests. Memory is O(1) just like Token Bucket.

### Step 3 — Update README interface section

File: `README.md`, section "Interfaz unificada".

Current (incomplete — missing Remaining):
```go
type RateLimiter interface {
    Allow() bool
    AllowN(n int) bool
    Reset()
}
```

Correct (add Remaining):
```go
type RateLimiter interface {
    Allow() bool        // permite 1 request
    AllowN(n int) bool  // permite n requests (batch)
    Reset()             // resetea al estado inicial
    Remaining() int     // slots restantes en la ventana actual
}
```

### Step 4 — Run tests

```bash
go test ./...
# Note: -race requires CGO (gcc) on Windows. Run without -race on Windows.
# On Linux/macOS: go test ./... -race
```

### Step 5 — Commit

```bash
git add -A
git commit -m "feat: complete M6+M7 — benchmarks, gateway, loadgen, skill docs"
```

## Key architectural decisions (do not revisit)

- **No -race on Windows in CI** — requires CGO. Tests pass without it; -race is for
  Linux/macOS only. The Stop hook in `.claude/settings.json` runs without -race.
- **Leaky bucket = meter variant** — not a real queue. O(1) memory. Mathematically
  equivalent to token bucket for accept/reject decisions.
- **Multi[K] no eviction** — limiters are never removed from the map. Fine for a lab
  with known clients. A TODO comment documents this in multi.go.
- **SlidingLog B/op = 0 at benchmark steady state** — not a bug. After warmup the
  slice reaches capacity; purge + append balance out. Real cost is peak held memory
  (requests_per_window × 24 bytes). Explained in benchmarks/results/README.md.
- **benchLogLimit = 1<<30** — SlidingLog benchmarks use an unlimited cap to force the
  accept+purge path. Using the normal limit would measure the cheap rejection path only.

## Files map (where everything lives)

```
internal/algorithms/ratelimiter.go          ← RateLimiter interface
internal/algorithms/tokenbucket/            ← capacity + float token counter
internal/algorithms/fixedwindow/            ← int counter + window reset
internal/algorithms/leakybucket/            ← int queue depth (meter, not queue)
internal/algorithms/slidinglog/             ← []time.Time log, O(n) memory
internal/algorithms/slidingcounter/         ← two-window weighted blend, O(1)
internal/limiter/multi.go                   ← Multi[K] with double-checked locking
cmd/gateway/main.go                         ← HTTP server, per-IP Multi[string]
cmd/loadgen/main.go                         ← traffic simulator, comparison table
benchmarks/bench_test.go                    ← go test -bench comparative harness
benchmarks/results/README.md                ← committed benchmark numbers
README.md                                   ← community-facing docs (Spanish)
CLAUDE.md                                   ← Claude Code contract
PRD.md                                      ← original product requirements
```
