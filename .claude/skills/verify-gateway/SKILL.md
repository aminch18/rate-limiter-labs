---
name: verify-gateway
description: >
  Use when verifying the HTTP gateway and load generator work end-to-end.
  Triggers on: "verify gateway", "run loadgen", "test the gateway", "show the comparison table",
  "does the gateway work", "run the demo".
allowed-tools: Read, Bash(go:*), Bash(curl:*)
---

## Context

The gateway (`cmd/gateway/main.go`) exposes one HTTP endpoint per algorithm.
The loadgen (`cmd/loadgen/main.go`) fires three traffic patterns and prints a comparison table.
This is the "interesting part" of the project — it shows behavioral differences between
algorithms under real traffic, not just microbenchmark numbers.

## What to verify

The PRD (Section 15) promises this output for the burst pattern:

```
Pattern: burst — 30 req all at once
Algorithm            Allowed   Denied   Allow%
─────────────────────────────────────────────
TokenBucket               20       10    66.7%   ← absorbs burst to capacity=20
FixedWindow               10       20    33.3%   ← hard cutoff at limit=10
LeakyBucket               20       10    66.7%   ← queue absorbs burst
SlidingLog                10       20    33.3%   ← exact, same as fixed here
SlidingCounter            10       20    33.3%   ← approx, same result
```

Key behavioral invariant: TokenBucket and LeakyBucket allow MORE requests during a burst
because capacity=20 (vs window-based algorithms with limit=10).

## Step-by-step

### 1. Start the gateway in the background

```bash
go run ./cmd/gateway &
GATEWAY_PID=$!
sleep 2   # wait for it to bind :8080
```

### 2. Verify it's up

```bash
curl -s http://localhost:8080/healthz
# Expected: {"status":"ok"}
```

### 3. Quick manual smoke test

```bash
curl -si http://localhost:8080/token-bucket | grep -E "HTTP|X-Rate|allowed"
# Expected: HTTP/1.1 200, X-RateLimit-Algorithm: TokenBucket, X-RateLimit-Remaining: <N>
```

### 4. Run the loadgen

```bash
go run ./cmd/loadgen
```

### 5. Evaluate output

Check the burst pattern row for TokenBucket:
- `allowed` should be ~20 (capacity), not ~10
- FixedWindow `allowed` should be ~10 (hard limit)
- These two numbers being DIFFERENT is the key visual proof

### 6. Stop the gateway

```bash
kill $GATEWAY_PID 2>/dev/null || true
```

## Known issues to watch for

### Issue A — loadgen notes column
`printTable()` in `cmd/loadgen/main.go` does NOT print a Notes column.
The README shows notes but the code doesn't. If you want them, add a `notes string`
field to the `result` struct and populate it in `printTable()`. Low priority.

### Issue B — window-based algorithms + burst timing
The loadgen sends 30 concurrent requests per algorithm sequentially, sleeping 1100ms
between algorithms so window limiters reset. If an algorithm's window is 1s and the
1100ms sleep is insufficient (clock drift, slow CI), the "denied" count can be lower
than expected. This is expected variance — the table is illustrative, not deterministic.

### Issue C — LeakyBucket capacity vs window limit
Gateway config: TokenBucket and LeakyBucket use capacity=20, rate=10/sec.
Window-based (Fixed, SlidingLog, SlidingCounter) use limit=10/sec.
This config mismatch is intentional — it's what makes the burst row interesting.
Do NOT equalize them.

## Success criteria

- Gateway starts without errors
- `/healthz` returns 200
- loadgen completes all 3 patterns without panicking
- Burst pattern shows TokenBucket/LeakyBucket allowing ~2× what FixedWindow allows
