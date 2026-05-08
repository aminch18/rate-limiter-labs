// Gateway exposes one HTTP endpoint per rate limiting algorithm.
// Requests are limited per client IP using a keyed Multi limiter so different
// clients never share quota. Each algorithm is configured identically so
// behavioral differences under load are purely algorithmic.
//
// Routes:
//
//	GET /token-bucket
//	GET /fixed-window
//	GET /leaky-bucket
//	GET /sliding-log
//	GET /sliding-counter
//	GET /stats
//	GET /healthz
//
// Run: go run ./cmd/gateway
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms"
	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/fixedwindow"
	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/leakybucket"
	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/slidingcounter"
	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/slidinglog"
	"github.com/tu-usuario/rate-limiter-labs/internal/algorithms/tokenbucket"
	"github.com/tu-usuario/rate-limiter-labs/internal/limiter"
)

const (
	listenAddr      = ":8080"
	shutdownTimeout = 5 * time.Second
)

// envInt reads an integer from the environment, falling back to def.
// This lets k8s ConfigMaps override algorithm parameters without rebuilding
// the image — e.g. WINDOW_LIMIT=1000 for load testing at realistic scale.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return def
}

// Algorithm parameters — override via environment variables.
// Defaults (10 req/s, capacity 20) are intentionally small so unit tests
// and local demos are easy to reason about. Set higher values in k8s
// ConfigMaps for realistic load testing (e.g. WINDOW_LIMIT=1000).
var (
	capacity    = envInt("CAPACITY", 20)
	windowLimit = envInt("WINDOW_LIMIT", 10)
	windowSecs  = envInt("WINDOW_SECS", 1)
	ratePerSec  = envFloat("RATE_PER_SEC", 10.0)
)

// endpoint pairs a per-IP keyed limiter with aggregate allow/deny counters.
type endpoint struct {
	urlKey  string
	name    string
	limiter *limiter.Multi[string]
	allowed atomic.Int64
	denied  atomic.Int64
}

func (e *endpoint) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		ok := e.limiter.Allow(ip)
		remaining := e.limiter.Remaining(ip)

		if ok {
			e.allowed.Add(1)
		} else {
			e.denied.Add(1)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Algorithm", e.name)
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if !ok {
			w.WriteHeader(http.StatusTooManyRequests)
		}

		resp := map[string]any{
			"algorithm": e.name,
			"allowed":   ok,
			"client_ip": ip,
			"remaining": remaining,
			"ts":        time.Now().UnixMilli(),
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			// WriteHeader already sent; log and continue
			log.Printf("encode response [%s]: %v", e.name, err)
		}
	}
}

// clientIP extracts the real client IP, honoring X-Forwarded-For for proxied deployments.
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

func buildEndpoints() []*endpoint {
	return []*endpoint{
		{
			urlKey: "token-bucket",
			name:   "TokenBucket",
			limiter: limiter.NewMulti[string](func() algorithms.RateLimiter {
				return tokenbucket.New(capacity, ratePerSec)
			}),
		},
		{
			urlKey: "fixed-window",
			name:   "FixedWindow",
			limiter: limiter.NewMulti[string](func() algorithms.RateLimiter {
				return fixedwindow.New(windowLimit, windowSecs)
			}),
		},
		{
			urlKey: "leaky-bucket",
			name:   "LeakyBucket",
			limiter: limiter.NewMulti[string](func() algorithms.RateLimiter {
				return leakybucket.New(capacity, ratePerSec)
			}),
		},
		{
			urlKey: "sliding-log",
			name:   "SlidingLog",
			limiter: limiter.NewMulti[string](func() algorithms.RateLimiter {
				return slidinglog.New(windowLimit, windowSecs)
			}),
		},
		{
			urlKey: "sliding-counter",
			name:   "SlidingCounter",
			limiter: limiter.NewMulti[string](func() algorithms.RateLimiter {
				return slidingcounter.New(windowLimit, windowSecs)
			}),
		},
	}
}

func statsHandler(eps []*endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		type stat struct {
			Algorithm   string `json:"algorithm"`
			Allowed     int64  `json:"allowed"`
			Denied      int64  `json:"denied"`
			TrackedKeys int    `json:"tracked_keys"`
		}
		out := make([]stat, len(eps))
		for i, ep := range eps {
			out[i] = stat{
				Algorithm:   ep.name,
				Allowed:     ep.allowed.Load(),
				Denied:      ep.denied.Load(),
				TrackedKeys: ep.limiter.Len(),
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(out); err != nil {
			log.Printf("encode stats: %v", err)
		}
	}
}

// metricsHandler exposes per-algorithm counters in Prometheus text format (v0.0.4).
// No external dependency — the format is plain text: "metric{labels} value\n".
// Prometheus scrapes this at /metrics; Grafana reads from Prometheus.
func metricsHandler(eps []*endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		fmt.Fprintln(w, "# HELP rate_limiter_requests_total Total requests handled by the gateway.")
		fmt.Fprintln(w, "# TYPE rate_limiter_requests_total counter")
		for _, ep := range eps {
			fmt.Fprintf(w, "rate_limiter_requests_total{algorithm=%q,result=\"allowed\"} %d\n", ep.name, ep.allowed.Load())
			fmt.Fprintf(w, "rate_limiter_requests_total{algorithm=%q,result=\"denied\"} %d\n", ep.name, ep.denied.Load())
		}

		fmt.Fprintln(w, "# HELP rate_limiter_active_keys Unique client IPs currently tracked per algorithm.")
		fmt.Fprintln(w, "# TYPE rate_limiter_active_keys gauge")
		for _, ep := range eps {
			fmt.Fprintf(w, "rate_limiter_active_keys{algorithm=%q} %d\n", ep.name, ep.limiter.Len())
		}
	}
}

// buildMux wires all endpoints into a ServeMux. Extracted for testability.
func buildMux(eps []*endpoint) *http.ServeMux {
	mux := http.NewServeMux()
	for _, ep := range eps {
		mux.HandleFunc("/"+ep.urlKey, ep.handler())
	}
	mux.HandleFunc("/stats", statsHandler(eps))
	mux.HandleFunc("/metrics", metricsHandler(eps))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"status":"ok"}` + "\n")); err != nil {
			log.Printf("healthz write: %v", err)
		}
	})
	return mux
}

func main() {
	eps := buildEndpoints()
	mux := buildMux(eps)

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second, // mitigate Slowloris: headers must arrive within 2s
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second, // keep-alive connections recycled after 30s
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("shutting down gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	log.Printf("gateway listening on %s", listenAddr)
	log.Printf("routes: /token-bucket /fixed-window /leaky-bucket /sliding-log /sliding-counter /stats /metrics /healthz")

	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
	log.Println("gateway stopped")
}
