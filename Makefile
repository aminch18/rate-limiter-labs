# Requiere GNU Make. En Windows: winget install GnuWin32.Make
# o usar los comandos go directamente (ver README.md → "Referencia de comandos")
.PHONY: help test bench lint fmt build gateway loadgen demo clean

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
