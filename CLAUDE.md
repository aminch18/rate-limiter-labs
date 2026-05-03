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
