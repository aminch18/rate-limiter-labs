# syntax=docker/dockerfile:1

# ── Stage 1: build ────────────────────────────────────────────────────────────
# golang:alpine is ~250 MB; scratch final image is ~10 MB.
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Copy module files first so Docker caches the download layer separately.
# This layer is only invalidated when go.mod or go.sum change — not on every code edit.
COPY go.mod .
RUN go mod download

COPY . .

# ARG CMD selects which binary to build: "gateway" (default) or "loadgen".
# Build both in CI with:
#   docker build --build-arg CMD=gateway -t rate-limiter-gateway .
#   docker build --build-arg CMD=loadgen -t rate-limiter-loadgen .
ARG CMD=gateway

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -trimpath \
    -o /bin/app \
    ./cmd/${CMD}

# ── Stage 2: runtime ──────────────────────────────────────────────────────────
# alpine over scratch because:
#   - wget is needed for Docker HEALTHCHECK (scratch has no shell or tools)
#   - ca-certificates handles HTTPS if loadgen ever hits a TLS-terminated gateway
#   - easier to exec into for debugging in staging
FROM alpine:3.20

RUN apk add --no-cache ca-certificates wget

# Run as non-root: principle of least privilege.
RUN adduser -D -u 1001 appuser
USER appuser

COPY --from=builder /bin/app /app

EXPOSE 8080

ENTRYPOINT ["/app"]
