# Rate Limiter Labs

Implementación en Go de todos los algoritmos principales de rate limiting bajo una interfaz unificada, con tests, benchmarks comparativos y un **gateway HTTP en vivo** que muestra cómo se comporta cada algoritmo bajo tráfico real.

> Proyecto de aprendizaje — no es una librería de producción.

---

## Quickstart

**Requisito:** Go 1.22+ → [go.dev/dl](https://go.dev/dl/)

```bash
# Clonar y entrar al repo
git clone https://github.com/tu-usuario/rate-limiter-labs
cd rate-limiter-labs

# Tests (deben pasar todos en verde con -race)
go test ./... -race

# Benchmarks comparativos
go test ./benchmarks/ -bench=. -benchmem -count=3
```

---

## Gateway en vivo (la parte interesante)

Un servidor HTTP con una ruta por algoritmo, más un generador de carga que lanza 3 patrones de tráfico y muestra la tabla comparativa.

```bash
# Terminal 1 — levantar el gateway
go run ./cmd/gateway
# → gateway listening on :8080

# Terminal 2 — lanzar el generador de carga
go run ./cmd/loadgen
```

Salida esperada:

```
Rate Limiter Labs — Live Comparison
Gateway: http://localhost:8080
Config:  capacity/limit=20 (token/leaky) or 10 req/sec (window-based)

Pattern: steady — 20 req @ 5/sec (below limit)
Algorithm             Allowed   Denied   Allow%
──────────────────────────────────────────────────────────────────
TokenBucket                20        0   100.0%
FixedWindow                20        0   100.0%
LeakyBucket                20        0   100.0%
SlidingLog                 20        0   100.0%
SlidingCounter             20        0   100.0%

Pattern: burst — 30 req all at once
Algorithm             Allowed   Denied   Allow%   Notes
──────────────────────────────────────────────────────────────────
TokenBucket                20       10    66.7%   ← absorbe burst hasta capacity=20
FixedWindow                10       20    33.3%   ← corte duro en el límite=10
LeakyBucket                20       10    66.7%   ← absorbe burst hasta capacity=20
SlidingLog                 10       20    33.3%   ← ventana deslizante exacta
SlidingCounter             10       20    33.3%   ← aproximación, mismo resultado

Pattern: overload — 60 req @ 20/sec (2× limit)
Algorithm             Allowed   Denied   Allow%   Notes
──────────────────────────────────────────────────────────────────
TokenBucket                49       11    81.7%   ← tokens se recargan durante la ráfaga
FixedWindow                30       30    50.0%   ← reset duro por ventana
LeakyBucket                45       15    75.0%   ← drain rate suaviza el input
SlidingLog                 30       30    50.0%   ← preciso, sin aproximación
SlidingCounter             30       30    50.0%   ← blend ponderado, O(1) memoria
```

También puedes probar cada endpoint manualmente:

```bash
curl -i http://localhost:8080/token-bucket
curl -i http://localhost:8080/fixed-window
curl -i http://localhost:8080/leaky-bucket
curl -i http://localhost:8080/sliding-log
curl -i http://localhost:8080/sliding-counter
```

Cada respuesta incluye:
- `X-RateLimit-Algorithm` — qué algoritmo procesó la request
- `X-RateLimit-Remaining` — slots estimados restantes
- HTTP `200` si se permite, `429` si se rechaza

---

## Los algoritmos

| Algoritmo | Memoria | Bursts | Boundary burst | Cuándo usarlo |
|---|---|---|---|---|
| Token Bucket | O(1) | Sí, hasta capacity | No | Caso general |
| Fixed Window | O(1) | No | **Sí (2× en boundary)** | Máxima simplicidad |
| Leaky Bucket | O(1) | Aplana bursts | No | Output rate uniforme |
| Sliding Window Log | O(n) por cliente | No | No | Precisión exacta |
| Sliding Window Counter | O(1) | No | Aprox. no | Híbrido práctico |

### Token Bucket

```go
rl := tokenbucket.New(20, 10.0) // capacity=20, refill 10 tokens/sec
```

El bucket acumula tokens hasta `capacity`. Cada request consume 1 token. Los bursts se absorben naturalmente hasta el límite del bucket.

### Fixed Window Counter

```go
rl := fixedwindow.New(10, 1) // 10 req por ventana de 1 segundo
```

El contador más simple. Resetea en cada ventana. Problema: un cliente puede enviar `2×limit` requests straddling dos ventanas consecutivas.

### Leaky Bucket

```go
rl := leakybucket.New(20, 10.0) // cola de 20, drena a 10/sec
```

Las requests llenan una cola; si está llena se rechazan. La cola drena a tasa constante → output perfectamente uniforme. Los bursts se aplanan completamente.

### Sliding Window Log

```go
rl := slidinglog.New(10, 1) // 10 req por ventana deslizante de 1 segundo
```

Guarda el timestamp de cada request aceptada. En cada llamada purga los timestamps fuera de la ventana y cuenta los restantes. Preciso, sin boundary bursts. Caro en memoria con alta carga (O(n) por cliente).

### Sliding Window Counter

```go
rl := slidingcounter.New(10, 1) // ~10 req por ventana deslizante de 1 segundo
```

Mantiene dos contadores de ventana fija (actual y anterior) y mezcla con peso:
```
estimado = prev × (1 - elapsed/window) + curr
```
O(1) memoria, precisión prácticamente equivalente al Sliding Log.

---

## Estructura del proyecto

```
rate-limiter-labs/
├── cmd/
│   ├── gateway/main.go          # HTTP server — un endpoint por algoritmo
│   └── loadgen/main.go          # Generador de carga + tabla comparativa
├── internal/algorithms/
│   ├── ratelimiter.go           # Interfaz RateLimiter
│   ├── tokenbucket/
│   ├── fixedwindow/
│   ├── leakybucket/
│   ├── slidinglog/
│   └── slidingcounter/
├── benchmarks/
│   ├── bench_test.go            # Benchmarks comparativos
│   └── results/README.md        # Resultados commiteados
├── CLAUDE.md                    # Contrato para sesiones de Claude Code
└── PRD.md                       # Product Requirements Document
```

---

## Interfaz unificada

Todos los algoritmos implementan:

```go
type RateLimiter interface {
    Allow() bool        // permite 1 request
    AllowN(n int) bool  // permite n requests (batch)
    Reset()             // resetea al estado inicial
    Remaining() int     // slots restantes en la ventana actual
}
```

Todas las implementaciones son seguras para uso concurrente.

---

## Cómo ejecutar los tests

### Tests unitarios — los algoritmos

Cada algoritmo tiene su propio `_test.go` con tests tabla-driven. Cubren: happy path, límite exacto, rechazo, `AllowN`, `Reset`, `Remaining` y concurrencia.

```bash
# Todos los paquetes de una vez
make test
# o directamente:
go test ./... -count=1 -timeout=60s

# Con detector de data races (requiere gcc; nativo en Linux/macOS, requiere CGO en Windows)
make test-race
# o:
go test ./... -race
```

Salida esperada — todos en verde:
```
ok  github.com/tu-usuario/rate-limiter-labs/internal/algorithms/fixedwindow
ok  github.com/tu-usuario/rate-limiter-labs/internal/algorithms/leakybucket
ok  github.com/tu-usuario/rate-limiter-labs/internal/algorithms/slidingcounter
ok  github.com/tu-usuario/rate-limiter-labs/internal/algorithms/slidinglog
ok  github.com/tu-usuario/rate-limiter-labs/internal/algorithms/tokenbucket
ok  github.com/tu-usuario/rate-limiter-labs/internal/limiter
```

---

### Benchmarks de algoritmos — comparativa de rendimiento

Mide `ns/op`, `B/op` y `allocs/op` para los 5 algoritmos bajo 3 perfiles de carga: steady, burst y concurrent.

```bash
# Benchmark completo (3 runs, toma la mediana)
make bench
# o directamente:
go test ./benchmarks/ -bench=. -benchmem -count=3

# Rápido durante desarrollo (1 run, sin memoria)
make bench-quick
```

Los resultados están commiteados en [`benchmarks/results/README.md`](benchmarks/results/README.md) con la explicación de cada número. Resumen:

```
Benchmark                  ns/op    B/op   allocs/op
──────────────────────────────────────────────────────
FixedWindow/Steady         11.9      0        0      ← más rápido, O(1)
SlidingCounter/Steady      18.6      0        0      ← híbrido, O(1)
TokenBucket/Steady         13.7      0        0      ← referencia, O(1)
LeakyBucket/Steady         24.0      0        0      ← output uniforme, O(1)
SlidingLog/Steady          40.2      0        0      ← exacto, O(n) memoria
```

> `SlidingLog` es 3–4× más lento. Su coste real no es por operación sino en **memoria residente**: `peticiones_en_ventana × 24 bytes` por cliente.

---

### Tests del gateway — comportamiento bajo tráfico real

Esto es lo más interesante: ver cómo cada algoritmo reacciona diferente al mismo tráfico.

**Paso 1 — Arrancar el gateway** (Terminal 1):
```bash
make gateway
# o: go run ./cmd/gateway
# → gateway listening on :8080
```

**Paso 2 — Lanzar el generador de carga** (Terminal 2):
```bash
make loadgen
# o: go run ./cmd/loadgen
```

El loadgen lanza 3 patrones y muestra la tabla comparativa:

```
Pattern: burst — 30 req all at once
Algorithm             Allowed   Denied   Allow%   Notes
──────────────────────────────────────────────────────────────────
TokenBucket                20       10    66.7%   ← absorbs burst up to capacity=20
FixedWindow                10       20    33.3%   ← hard cutoff at limit=10
LeakyBucket                20       10    66.7%   ← absorbs burst up to capacity=20
SlidingLog                 10       20    33.3%   ← exact sliding window, limit=10
SlidingCounter             10       20    33.3%   ← approx sliding window, limit=10
```

**La diferencia clave:** TokenBucket y LeakyBucket tienen `capacity=20` y absorben el burst. Los algoritmos de ventana tienen `limit=10` y cortan en duro. Esa diferencia de comportamiento es lo que los benchmarks de ns/op no te cuentan.

También puedes apuntar el loadgen a un gateway remoto:
```bash
make loadgen-remote ADDR=host:port
```

---

## Referencia de comandos

> **Windows:** `make` requiere GNU Make (`winget install GnuWin32.Make`). Alternativamente usa los comandos `go` directamente — están todos documentados en el `Makefile`.

```bash
make test           # tests unitarios
make test-race      # tests con -race (requiere gcc en Windows)
make bench          # benchmarks comparativos (3 runs)
make bench-quick    # benchmark rápido (1 run)
make lint           # go vet
make fmt            # gofmt
make build          # compila binarios en bin/
make gateway        # arranca gateway en :8080
make loadgen        # lanza generador de carga
make demo           # instrucciones para el demo completo
make clean          # elimina binarios
```
