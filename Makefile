# Requiere GNU Make. En Windows: winget install GnuWin32.Make
# o usar los comandos go directamente (ver README.md → "Referencia de comandos")
.PHONY: help test bench lint fmt build gateway loadgen demo clean \
        docker-build docker-up docker-down docker-logs k8s-apply k8s-delete

# Default target
help:
	@echo ""
	@echo "Rate Limiter Labs — comandos disponibles"
	@echo ""
	@echo "  make test        Ejecuta todos los tests unitarios"
	@echo "  make bench       Ejecuta benchmarks comparativos (ns/op, B/op, allocs/op)"
	@echo "  make lint        go vet en todos los paquetes"
	@echo "  make fmt         gofmt en todos los ficheros"
	@echo "  make build       Compila gateway y loadgen"
	@echo "  make gateway     Arranca el gateway en :8080"
	@echo "  make loadgen     Lanza el generador de carga contra localhost:8080"
	@echo "  make demo        Arranca gateway + loadgen juntos (necesita dos terminales)"
	@echo "  make clean       Elimina binarios compilados"
	@echo ""
	@echo "Docker"
	@echo "  make docker-build  Construye las imágenes gateway y loadgen"
	@echo "  make docker-up     Levanta gateway + loadgen con docker compose"
	@echo "  make docker-down   Para y elimina los contenedores"
	@echo "  make docker-logs   Tail de logs del gateway"
	@echo ""
	@echo "Kubernetes"
	@echo "  make k8s-apply   Aplica los manifiestos en k8s/"
	@echo "  make k8s-logs    Logs del job loadgen en tiempo real"
	@echo "  make k8s-delete  Elimina todos los recursos de k8s/"
	@echo ""

# ── Tests ─────────────────────────────────────────────────────────────────────

# Ejecuta todos los tests unitarios de todos los paquetes.
# Nota: -race requiere CGO (gcc). En Windows sin gcc omitir -race o usar WSL.
test:
	go test ./... -count=1 -timeout=60s

# Igual que test pero con -race (requiere gcc en Windows; nativo en Linux/macOS)
test-race:
	go test ./... -race -count=1 -timeout=60s

# ── Benchmarks ────────────────────────────────────────────────────────────────

# Benchmarks comparativos de los 5 algoritmos: ns/op, B/op, allocs/op.
# -count=3 ejecuta 3 veces y puedes tomar la mediana.
# Los resultados commiteados están en benchmarks/results/README.md
bench:
	go test ./benchmarks/ -bench=. -benchmem -count=3

# Benchmark rápido (1 run, sin memoria) para iterar rápido durante desarrollo
bench-quick:
	go test ./benchmarks/ -bench=. -count=1

# ── Calidad ───────────────────────────────────────────────────────────────────

lint:
	go vet ./...

fmt:
	gofmt -w .

# ── Gateway ───────────────────────────────────────────────────────────────────

build:
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/loadgen ./cmd/loadgen

# Arranca el gateway en :8080 (Ctrl+C para parar)
gateway:
	go run ./cmd/gateway

# Lanza los 3 patrones de tráfico contra el gateway en localhost:8080
loadgen:
	go run ./cmd/loadgen

# Loadgen apuntando a un gateway remoto: make loadgen-remote ADDR=host:port
loadgen-remote:
	go run ./cmd/loadgen -addr $(ADDR)

# Instrucciones para el demo completo (requiere dos terminales)
demo:
	@echo ""
	@echo "Demo del gateway en vivo — abre dos terminales:"
	@echo ""
	@echo "  Terminal 1:  make gateway"
	@echo "  Terminal 2:  make loadgen"
	@echo ""
	@echo "O en una sola línea (background):"
	@echo "  go run ./cmd/gateway & sleep 2 && go run ./cmd/loadgen"
	@echo ""

clean:
	rm -rf bin/

# ── Docker ────────────────────────────────────────────────────────────────────

# Build both images locally (no registry push).
docker-build:
	docker build --build-arg CMD=gateway -t rate-limiter-gateway:local .
	docker build --build-arg CMD=loadgen -t rate-limiter-loadgen:local .

# Start gateway + run loadgen via docker compose.
# Loadgen exits after printing the table; gateway keeps running.
# Use --abort-on-container-exit to stop everything when loadgen finishes.
docker-up:
	docker compose up --build --abort-on-container-exit

# Stop and remove containers (keeps images).
docker-down:
	docker compose down

# Tail gateway logs.
docker-logs:
	docker compose logs -f gateway

# ── Kubernetes ────────────────────────────────────────────────────────────────

# Apply all manifests. Set IMAGE_TAG before calling if pushing to a registry:
#   make k8s-apply IMAGE_TAG=ghcr.io/tu-usuario/rate-limiter-gateway:v1.0
IMAGE_TAG ?= rate-limiter-gateway:latest

k8s-apply:
	kubectl apply -f k8s/gateway.yaml
	kubectl apply -f k8s/loadgen-job.yaml

# Watch the loadgen job output live.
k8s-logs:
	kubectl wait --for=condition=ready pod -l job-name=loadgen --timeout=30s
	kubectl logs -f job/loadgen

k8s-delete:
	kubectl delete -f k8s/ --ignore-not-found
