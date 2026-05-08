# Internals — Design Decisions

> Every line explained. Not a Go tutorial — a guide to understanding *why* each decision was made.

← Back to [README](../README.md)

---

## 1. La interfaz (`internal/algorithms/ratelimiter.go`)

```go
type RateLimiter interface {
    Allow() bool
    AllowN(n int) bool
    Reset()
    Remaining() int
}
```

**Por qué una interfaz y no structs directos:**
Todos los algoritmos son intercambiables desde fuera. El gateway, el Multi[K] y los benchmarks
solo saben que tienen un `RateLimiter` — no saben si es Token Bucket o Sliding Log.
Esto es el patrón de diseño más importante del proyecto.

**`Allow()` devuelve bool, no error:**
Un rate limiter nunca falla en el sentido de error de sistema — simplemente acepta o rechaza.
Si devolviera `error` forzarías a cada caller a manejar un error que nunca ocurre.

**`AllowN(n int)`:**
Batch operations. Si un cliente sube un fichero de 5 MB y cobras 1 token por KB,
necesitas consumir 5000 tokens de una vez. `Allow()` equivale exactamente a `AllowN(1)`.

**`Reset()`:**
Solo útil en tests y benchmarks. Entre un benchmark y otro necesitas el limiter en estado limpio.
En producción nunca deberías resetear un limiter en caliente.

**`Remaining() int`:**
Permite devolver el header `X-RateLimit-Remaining` en el gateway. El valor es exacto
en algoritmos de contador (Fixed Window, Sliding Log) y aproximado en Token Bucket y
Sliding Counter (dependen de tiempo continuo, no de contadores discretos).

---

### `Wait(ctx, rl)` — la función de blocking

```go
func Wait(ctx context.Context, rl RateLimiter) error {
    const (
        minPoll = 1 * time.Millisecond
        maxPoll = 100 * time.Millisecond
    )
    poll := minPoll
    for {
        if rl.Allow() {
            return nil
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(poll):
            if poll < maxPoll {
                poll *= 2
            }
        }
    }
}
```

**Por qué es una función y no un método de la interfaz:**
Si fuera un método de la interfaz, cada uno de los 5 algoritmos tendría que implementarlo.
Pero la lógica de espera es siempre la misma: llamar `Allow()` en bucle con pausa.
Al ponerlo fuera de la interfaz, funciona con cualquier `RateLimiter` sin tocar sus implementaciones.
Es composición sobre herencia.

**El backoff exponencial (1ms → 100ms):**
Sin backoff, `Wait` haría un spin-loop: millones de llamadas por segundo que consumen CPU.
Con backoff fijo grande (ej. 100ms), añades demasiada latencia cuando el token llega en 2ms.
El backoff exponencial es el equilibrio: primero intenta rápido, luego se calma.

**`<-ctx.Done()`:**
Context es el mecanismo de cancelación estándar de Go. Si el cliente HTTP cierra la conexión,
el context se cancela y `Wait` retorna en el siguiente ciclo del select, sin bloquear para siempre.

---

## 2. Token Bucket (`internal/algorithms/tokenbucket/tokenbucket.go`)

```go
type TokenBucket struct {
    mu       sync.Mutex
    capacity float64
    tokens   float64
    rate     float64
    lastTime time.Time
}
```

**`tokens` es `float64`, no `int`:**
Los tokens se acumulan de forma continua. Si la tasa es 10 tokens/segundo y han pasado 50ms,
se añaden exactamente 0.5 tokens. Con `int` perderías esa fracción y el algoritmo sería
menos preciso — especialmente a tasas bajas.

**`lastTime time.Time`:**
No hay goroutine de "recarga". En lugar de recargar en background, calculamos
cuántos tokens se han acumulado desde la última llamada cuando llega una nueva request.
Es el patrón "lazy refill" — más eficiente que tener una goroutine despertando cada X ms.

### La función `refill()`

```go
func (tb *TokenBucket) refill() {
    now := time.Now()
    elapsed := now.Sub(tb.lastTime).Seconds()
    tb.tokens = min(tb.capacity, tb.tokens + elapsed*tb.rate)
    tb.lastTime = now
}
```

**`elapsed * tb.rate`:**
Si han pasado `elapsed` segundos y la tasa es `rate` tokens/segundo,
se añaden exactamente `elapsed × rate` tokens. Es física básica: velocidad × tiempo = distancia.

**`min(tb.capacity, ...)`:**
El bucket no puede tener más tokens que su capacidad. Sin este cap, un servidor
que estuvo apagado 1 hora acumularía 36000 tokens y permitiría un burst enorme.

### `AllowN(n)`

```go
func (tb *TokenBucket) AllowN(n int) bool {
    tb.mu.Lock()
    defer tb.mu.Unlock()
    tb.refill()
    if tb.tokens < float64(n) {
        return false
    }
    tb.tokens -= float64(n)
    return true
}
```

**`mu.Lock()` + `defer mu.Unlock()`:**
`defer` garantiza que el mutex se libere incluso si hay un panic. Sin `defer`, un panic entre
`Lock()` y `Unlock()` dejaría el mutex bloqueado para siempre (deadlock permanente).

---

## 3. Fixed Window (`internal/algorithms/fixedwindow/fixedwindow.go`)

```go
type FixedWindow struct {
    mu          sync.Mutex
    limit       int
    windowSecs  int
    count       int
    windowStart time.Time
}
```

El más simple de los 5. Un contador que se resetea cada `windowSecs` segundos.

### El problema del boundary burst

Si el límite es 10 req/segundo y la ventana va de T=0 a T=1:
- A T=0.9: envías 10 requests → todas pasan (ventana actual vacía)
- A T=1.1: envías 10 más → todas pasan (nueva ventana, contador resetea)

En 0.2 segundos pasaron 20 requests. El doble del límite.
Esto es el "boundary burst problem" que los algoritmos de ventana deslizante resuelven.

### `maybeReset()`

```go
func (fw *FixedWindow) maybeReset(now time.Time) {
    if now.Sub(fw.windowStart) >= time.Duration(fw.windowSecs)*time.Second {
        fw.count = 0
        fw.windowStart = now
    }
}
```

Resetea solo cuando ha pasado una ventana completa desde `windowStart`, no desde "ahora".
Si usaras `time.Now()` para el nuevo `windowStart`, la ventana podría desplazarse arbitrariamente.

---

## 4. Leaky Bucket (`internal/algorithms/leakybucket/leakybucket.go`)

```go
type LeakyBucket struct {
    mu       sync.Mutex
    capacity int
    rate     float64
    queue    int        // número de tokens en la "cola"
    lastDrain time.Time
}
```

**Por qué `queue int` y no una slice de requests reales:**
Esta implementación usa la variante "como metro" (meter), no "como cola" (queue).
En la variante queue guardarías cada request en memoria y las procesarías una a una.
En la variante meter simplemente cuentas cuántas "gotas" hay en el balde — O(1) memoria.
La decisión de aceptar/rechazar es matemáticamente equivalente.

### `drain()`

```go
func (lb *LeakyBucket) drain(now time.Time) {
    elapsed := now.Sub(lb.lastDrain).Seconds()
    drained := int(elapsed * lb.rate)
    if drained > 0 {
        lb.queue -= drained
        if lb.queue < 0 {
            lb.queue = 0
        }
        lb.lastDrain = now
    }
}
```

**`int(elapsed * lb.rate)` — truncamiento deliberado:**
Solo drenamos gotas enteras. Una fracción de gota no se drena.
Por eso solo actualizamos `lastDrain` cuando al menos 1 gota se ha drenado (`if drained > 0`).
Si actualizáramos `lastDrain` siempre, perderíamos la fracción acumulada y el drenaje
sería más lento de lo esperado.

---

## 5. Sliding Window Log (`internal/algorithms/slidinglog/slidinglog.go`)

```go
type SlidingLog struct {
    mu         sync.Mutex
    limit      int
    windowSecs int
    log        []time.Time
}
```

**`log []time.Time`:**
El único algoritmo con memoria O(n). Cada request aceptada deja su timestamp en el slice.
24 bytes por `time.Time` (wall clock + monotonic). A 1000 req/seg en una ventana de 1s:
1000 × 24 bytes = 24 KB por cliente.

### `purge()`

```go
func (sl *SlidingLog) purge(now time.Time) {
    cutoff := now.Add(-time.Duration(sl.windowSecs) * time.Second)
    i := 0
    for i < len(sl.log) && sl.log[i].Before(cutoff) {
        i++
    }
    sl.log = sl.log[i:]
}
```

**`sl.log = sl.log[i:]` — re-slice sin copiar:**
Avanzamos el puntero de inicio del slice sin mover datos en memoria. Es O(1) por operación de purga,
pero el slice sigue apuntando al mismo array subyacente — la memoria de los primeros `i` elementos
no se libera hasta que el garbage collector detecta que el array completo ya no es accesible.

**Por qué B/op = 0 en los benchmarks a pesar de ser O(n):**
Después de ~1 segundo de warmup, el slice alcanza su capacidad de trabajo: purge elimina
entradas tan rápido como Allow las añade. En ese estado estable, append nunca necesita
reallocar el array — escribe en espacio ya reservado. El coste real es la memoria
**mantenida**, no las allocations por operación.

---

## 6. Sliding Window Counter (`internal/algorithms/slidingcounter/slidingcounter.go`)

```go
type SlidingCounter struct {
    mu          sync.Mutex
    limit       int
    windowSecs  int
    prevCount   int
    currCount   int
    windowStart time.Time
}
```

El algoritmo más inteligente en relación coste/precisión: O(1) memoria, sin boundary burst.

### La fórmula de estimación

```go
elapsed := time.Since(sc.windowStart).Seconds() / float64(sc.windowSecs)
estimated := float64(sc.prevCount)*(1-elapsed) + float64(sc.currCount)
```

**Qué significa `(1 - elapsed)`:**
Si llevamos el 30% de la ventana actual, el 70% de la ventana anterior todavía "cuenta".
Es una interpolación lineal: cuanto más avanzamos en la ventana actual, menos peso
tiene la ventana anterior. Simple, efectivo, nunca exacto pero siempre justo.

### `maybeAdvance()` — el bug más sutil del proyecto

```go
func (sc *SlidingCounter) maybeAdvance() {
    windowDur := time.Duration(sc.windowSecs) * time.Second
    if time.Since(sc.windowStart) < windowDur {
        return
    }
    sc.prevCount = sc.currCount
    sc.currCount = 0
    sc.windowStart = sc.windowStart.Add(windowDur) // ← CRÍTICO
    if time.Since(sc.windowStart) >= windowDur {
        sc.prevCount = 0
        sc.windowStart = time.Now()
    }
}
```

**`sc.windowStart.Add(windowDur)` y NO `time.Now()`:**
Si usaras `time.Now()`, el siguiente reset se calcularía desde ahora,
perdiendo el tiempo fraccionario ya transcurrido de la nueva ventana.
Ejemplo: ventana de 1s, windowStart=T=0, ahora T=1.3s.
- Con `time.Now()`: la siguiente ventana empieza en T=1.3, la estimación usa 0% de peso para la anterior.
- Con `Add(windowDur)`: la siguiente ventana empieza en T=1.0, correctamente han transcurrido 0.3s de ella.
El primer enfoque hacía que `TestSlidingCounter_WindowAdvance` fallara.

---

## 7. Multi[K] (`internal/limiter/multi.go`)

### La estructura

```go
type entry struct {
    rl       algorithms.RateLimiter
    lastSeen atomic.Int64  // Unix nanoseconds
}
```

**`atomic.Int64` para `lastSeen`:**
La eviction necesita saber cuándo se usó por última vez cada clave.
Si usáramos un campo `time.Time` normal, actualizarlo requeriría un write lock en cada `Allow()`.
Con `atomic.Int64` (nanosegundos Unix), la actualización es lock-free — 1 instrucción de CPU.
El read lock sigue siendo válido para leer el mapa.

### Double-checked locking en `get()`

```go
func (m *Multi[K]) get(key K) *entry {
    m.mu.RLock()
    e, ok := m.limits[key]
    m.mu.RUnlock()
    if ok {
        e.touch()
        return e  // fast path: la clave ya existe
    }

    m.mu.Lock()           // slow path: necesitamos crear la clave
    defer m.mu.Unlock()
    if e, ok = m.limits[key]; ok {  // ← re-check obligatorio
        e.touch()
        return e
    }
    e = &entry{rl: m.factory()}
    e.touch()
    m.limits[key] = e
    return e
}
```

**Por qué el segundo check dentro del write lock:**
Entre el `RUnlock()` y el `Lock()`, otra goroutine puede haber creado la misma clave.
Sin el re-check, crearías dos limiters para la misma clave y uno se perdería,
rompiendo el aislamiento de cuotas.

### Eviction

```go
func (m *Multi[K]) evictLoop(interval, ttl time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            m.evict(ttl)
        case <-m.stopCh:
            return
        }
    }
}
```

**Por qué `stopCh chan struct{}` y no `context.Context`:**
Un channel cerrado es el idioma Go para "broadcast de parada".
Podríamos usar context, pero stopCh es más explícito para este caso de uso interno.
`sync.Once` en `Stop()` garantiza que cerrar el channel dos veces no cause panic.

**Por qué `select` con dos cases:**
Si usaras solo `for { ticker.C; evict() }` sin select, la goroutine nunca terminaría.
El case `<-m.stopCh` es la única forma de salir del bucle cuando Stop() se llama.

---

## 8. Gateway (`cmd/gateway/main.go`)

### `clientIP()`

```go
func clientIP(r *http.Request) string {
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        if i := strings.IndexByte(xff, ','); i != -1 {
            return strings.TrimSpace(xff[:i])
        }
        return strings.TrimSpace(xff)
    }
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        return r.RemoteAddr
    }
    return host
}
```

**`X-Forwarded-For`:**
Cuando el gateway está detrás de un load balancer o proxy (nginx, AWS ALB),
`r.RemoteAddr` es la IP del proxy, no del cliente real.
XFF contiene la cadena de IPs: `client, proxy1, proxy2`.
Tomamos solo el primer elemento — el cliente original.

**Por qué no simplemente `r.RemoteAddr`:**
En producción, si usas RemoteAddr todos los clientes detrás del mismo proxy
comparten la misma IP y la misma cuota. Rate limiting por IP no funciona.

### Graceful shutdown

```go
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

go func() {
    <-quit
    ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
    defer cancel()
    srv.Shutdown(ctx)
}()
```

**Por qué buffered channel (`make(chan os.Signal, 1)`):**
Si el signal llega antes de que la goroutine esté leyendo del canal,
un unbuffered channel perdería la señal. El buffer de 1 garantiza que no se pierda.

**`Shutdown(ctx)` vs `Close()`:**
`Close()` cierra el listener y mata todas las conexiones activas inmediatamente.
`Shutdown(ctx)` deja de aceptar nuevas conexiones pero espera a que las activas terminen,
con un timeout para no esperar indefinidamente.

### `ReadHeaderTimeout: 2s`

```go
srv := &http.Server{
    ReadHeaderTimeout: 2 * time.Second,
    ...
}
```

Mitigación del ataque Slowloris: un cliente malicioso puede abrir conexiones y enviar
los headers extremadamente lento, consumiendo sockets. Con este timeout, si los headers
no llegan en 2 segundos, la conexión se cierra.

---

## 9. Loadgen (`cmd/loadgen/main.go`)

### `time.Ticker` vs `time.Sleep`

```go
ticker := time.NewTicker(interval)
defer ticker.Stop()
for i := 0; i < n; i++ {
    <-ticker.C
    fire()
}
```

**Por qué Ticker es más preciso que Sleep:**
`time.Sleep(200ms)` pausa la goroutine 200ms *después de ejecutar el código anterior*.
Si `fire()` tarda 5ms, el intervalo real es 205ms.
`ticker.C` dispara cada 200ms desde la creación del ticker, independientemente de cuánto
tarde el código entre ticks. El intervalo acumulado no deriva.

### Los 3 patrones de tráfico

**`steady` — 20 req @ 5/sec:**
Todos los algoritmos deberían pasar 100%. Sirve para verificar que no hay
falsos positivos — el sistema no rechaza tráfico legítimo bien por debajo del límite.

**`burst` — 30 req simultáneos:**
Aquí se ve la diferencia clave: Token Bucket y Leaky Bucket tienen `capacity=20`
y absorben el burst. Fixed Window, Sliding Log y Sliding Counter tienen `limit=10`
y cortan en duro. 20 vs 10 allowed — esa diferencia es el argumento de venta del Token Bucket.

**`overload` — 60 req @ 20/sec (2× límite):**
Muestra el comportamiento bajo carga sostenida. Token Bucket permite más porque
los tokens se recargan durante los 3 segundos que dura el test (60 req / 20 req/s).
Los algoritmos de ventana fija solo permiten `limit` por ventana sin importar cuánto tiempo pase.

---

## 10. Tests de integración del gateway (`cmd/gateway/gateway_test.go`)

```go
func newTestServer(t *testing.T) *httptest.Server {
    t.Helper()
    eps := buildEndpoints()
    srv := httptest.NewServer(buildMux(eps))
    t.Cleanup(srv.Close)
    return srv
}
```

**`httptest.NewServer` vs mock:**
Arranca un servidor HTTP real en un puerto efímero (:0 — el OS elige el puerto).
Los tests prueban el stack completo: routing, headers, serialización JSON, lógica de rate limiting.
Un mock solo probaría partes aisladas.

**`t.Cleanup(srv.Close)`:**
El server se cierra automáticamente cuando el test termina, sin importar si pasó o falló.
Equivalente a `defer srv.Close()` pero más robusto con subtests.

**`t.Helper()`:**
Cuando un test falla dentro de `newTestServer`, el error se reporta en la línea del test
que llamó a `newTestServer`, no dentro de la helper. Sin `t.Helper()` verías línea 8
en lugar de la línea real del test — confuso al debuggear.

```go
const limit = windowLimit  // constante del paquete main
```

**Por qué referenciar la constante del paquete y no hardcodear 10:**
Si cambian la config del gateway, el test sigue siendo correcto sin modificarse.
El test describe la invariante ("después de `limit` requests debe dar 429"),
no el valor concreto.
