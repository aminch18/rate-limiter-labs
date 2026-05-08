# Rate Limiter Labs

[![CI](https://github.com/aminch18/rate-limiter-labs/actions/workflows/ci.yml/badge.svg)](https://github.com/aminch18/rate-limiter-labs/actions/workflows/ci.yml)
[![Load Test](https://github.com/aminch18/rate-limiter-labs/actions/workflows/load-test.yml/badge.svg)](https://github.com/aminch18/rate-limiter-labs/actions/workflows/load-test.yml)
[![Go 1.22+](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Five rate limiting algorithms implemented from scratch in Go — same interface, same load, measurable differences.

---

## Why this exists

A few years ago I ran a .NET reverse proxy in production. Under spike traffic, open connections accumulated until the OS hit its limit — the service fell over. We migrated to a proper load balancer and API gateway, but the question stayed with me: **what exactly is happening inside those rate limiters, and why do different algorithms behave so differently under the same load?**

This repo is the answer. Not a library, not a framework — a lab where you can run the same traffic pattern against five algorithms and watch the numbers diverge.

---

## The five algorithms

| Algorithm | Memory | Burst tolerance | Boundary burst | Best for |
|-----------|--------|-----------------|----------------|----------|
| **Token Bucket** | O(1) | ✅ up to `capacity` | ✗ | General purpose — the industry default |
| **Fixed Window** | O(1) | ✗ | ⚠️ 2× at boundary | Simplest implementation, internal services |
| **Leaky Bucket** | O(1) | ✅ flattens completely | ✗ | Smooth output rate, payment processing |
| **Sliding Log** | O(n) | ✗ | ✗ | Exact enforcement, audit-grade precision |
| **Sliding Counter** | O(1) | ✗ | ~✗ approx | Best O(1) accuracy, production sweet spot |

All implement the same interface:

```go
type RateLimiter interface {
    Allow() bool
    AllowN(n int) bool
    Reset()
    Remaining() int
}
```

---

## Benchmark results

Measured on Intel i7-13700F, Go 1.22, `go test -bench=. -benchmem -count=3`.

```
Algorithm              Steady ns/op   Burst ns/op   Concurrent ns/op   Memory
─────────────────────────────────────────────────────────────────────────────
FixedWindow            11.9           19.9          76.6               O(1)
TokenBucket            13.7           13.9          40.2               O(1)
SlidingCounter         18.6           18.1          84.7               O(1)
LeakyBucket            24.0           23.4          89.9               O(1)
SlidingLog             40.2           206.6         106.8              O(n) ← 3–4× slower
```

`SlidingLog` is the only algorithm whose **resident memory scales with traffic**: ~24 KB per client at 1 000 req/s. All others hold a handful of fields regardless of load. The trade-off: it is the only mathematically exact sliding window — no approximation, no boundary bursts.

→ Full analysis in [`benchmarks/results/README.md`](benchmarks/results/README.md)

---

## Quick start

**Requirements:** Go 1.22+ · Docker (optional) · kind (optional)

```bash
git clone https://github.com/aminch18/rate-limiter-labs
cd rate-limiter-labs

# Unit tests — all green, including race detector
go test ./... -race

# Benchmarks
go test ./benchmarks/ -bench=. -benchmem -count=3
```

---

## See the algorithms behave differently

This is the interesting part. Start the gateway and hit it with three traffic patterns:

```bash
# Terminal 1
go run ./cmd/gateway

# Terminal 2
go run ./cmd/loadgen
```

Output:

```
Pattern: burst — 30 req all at once
Algorithm             Allowed   Denied   Allow%   Notes
──────────────────────────────────────────────────────────────────
TokenBucket                20       10    66.7%   ← absorbs burst to capacity=20
FixedWindow                10       20    33.3%   ← hard cut at limit=10
LeakyBucket                20       10    66.7%   ← absorbs burst to capacity=20
SlidingLog                 10       20    33.3%   ← exact sliding window
SlidingCounter             10       20    33.3%   ← approximation, same result

Pattern: overload — 60 req @ 20/sec (2× limit)
Algorithm             Allowed   Denied   Allow%   Notes
──────────────────────────────────────────────────────────────────
TokenBucket                49       11    81.7%   ← tokens refill during the run
FixedWindow                30       30    50.0%   ← hard reset per window
LeakyBucket                45       15    75.0%   ← drain rate smooths input
SlidingLog                 30       30    50.0%   ← precise, no approximation
SlidingCounter             30       30    50.0%   ← weighted blend, O(1) memory
```

The gateway exposes per-algorithm endpoints with Prometheus metrics:

```bash
curl -i http://localhost:8080/token-bucket    # X-RateLimit-Remaining, X-RateLimit-Algorithm
curl    http://localhost:8080/stats           # JSON: allowed/denied per algorithm
curl    http://localhost:8080/metrics         # Prometheus text format
```

---

## Load testing (k6)

Three scenarios against all five algorithms simultaneously, each VU with its own IP so the rate limiter sees independent clients:

- **Steady** — 5 VUs well below limit → all algorithms allow 100%
- **Burst** — spike to 50 VUs → Token/Leaky Bucket absorb more than window-based algorithms
- **Overload** — sustained 20 VUs at 2× limit → shows per-algorithm degradation pattern

### Three ways to run

**Local (no cluster needed):**
```bash
go run ./cmd/gateway &
k6 run k6/load_test.js
```

**Kind (local Kubernetes, free):**
```bash
make kind-up kind-load kind-deploy
make kind-test      # k6 against a real k8s cluster on your machine
make kind-down
```

**GitHub Actions (automated, free):**

Actions → *Load Test (k6 + kind)* → Run workflow

Choose **Historia A** (limit=10, algorithm differences visible) or **Historia B** (limit=1 000, infrastructure scale). Choose **replicas > 1** to demonstrate the distributed rate limiting problem live.

→ Full instructions in [`docs/load-testing.md`](docs/load-testing.md)

---

## The distributed rate limiting problem

With more than one gateway replica, in-memory rate limiting breaks:

```
Pod 1 → its own counter → limit = 1 000 req/s
Pod 2 → its own counter → limit = 1 000 req/s
Pod 3 → its own counter → limit = 1 000 req/s

Result: a client can send 3 000 req/s without being rejected
```

Run the load test with `replicas=3` to see this live. The GitHub Actions summary will flag it explicitly. This is the concrete motivation for Redis-backed distributed rate limiting — the natural next step beyond this lab.

---

## Docker

```bash
# Gateway + loadgen + Prometheus + Grafana
docker compose up --build --abort-on-container-exit

# Grafana: http://localhost:3000  (anonymous admin, Prometheus pre-wired)
# Prometheus: http://localhost:9090
```

---

## Project structure

```
rate-limiter-labs/
├── cmd/
│   ├── gateway/          # HTTP server — one endpoint per algorithm + /metrics /stats /healthz
│   └── loadgen/          # Go load generator — 3 traffic patterns, comparison table
├── internal/
│   ├── algorithms/       # RateLimiter interface + 5 implementations
│   └── limiter/          # Multi[K] — per-key limiter with TTL eviction
├── benchmarks/           # Comparative benchmarks + committed results
├── k6/                   # k6 load test (3 scenarios, per-VU IP, custom metrics)
├── k8s/                  # Kubernetes manifests (namespace, gateway, HPA, monitoring)
├── terraform/            # Hetzner k3s provisioning
├── config/               # Prometheus + Grafana provisioning for docker-compose
├── docs/                 # Deep-dive documentation
│   ├── internals.md      # Every design decision explained line by line
│   ├── load-testing.md   # Load testing guide — local, kind, Hetzner
│   └── decisions.md      # Original PRD — why this project was built this way
└── .github/workflows/
    ├── ci.yml            # Tests + build on every push/PR (ubuntu + macos)
    └── load-test.yml     # k6 on kind — manual + weekly schedule
```

---

## Commands

```bash
# Development
make test           # go test ./... -count=1 -timeout=60s
make test-race      # with race detector (requires gcc on Windows)
make bench          # benchmarks — 3 runs, ns/op B/op allocs/op
make lint           # go vet ./...
make fmt            # gofmt -w .
make build          # compile gateway + loadgen to bin/

# Local demo
make gateway        # start gateway on :8080
make loadgen        # run load generator
make k6             # k6 against local gateway

# Kind (local k8s)
make kind-up        # create cluster
make kind-load      # build + load image into kind
make kind-deploy    # apply manifests, wait for rollout
make kind-test      # k6 against kind cluster
make kind-down      # destroy cluster

# Docker
make docker-up      # gateway + loadgen + prometheus + grafana
make docker-down

# Hetzner k3s
make tf-init && make tf-apply
```

---

## Documentation

| Doc | What it covers |
|-----|---------------|
| [`docs/internals.md`](docs/internals.md) | Every design decision explained — why `float64` for tokens, why double-checked locking, why `Ticker` over `Sleep`, the `maybeAdvance` bug |
| [`docs/load-testing.md`](docs/load-testing.md) | Load testing guide — local k6, kind, GitHub Actions, Hetzner |
| [`docs/decisions.md`](docs/decisions.md) | Original PRD — context for why this was built, algorithm trade-offs, design constraints |
| [`benchmarks/results/README.md`](benchmarks/results/README.md) | Benchmark results with full statistical interpretation |
