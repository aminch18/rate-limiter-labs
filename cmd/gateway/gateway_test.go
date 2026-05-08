package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer starts a real HTTP server on a random port backed by all 5 endpoints.
// Uses httptest.NewServer so the OS picks the port — tests never conflict with each other
// or with a running gateway.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	eps := buildEndpoints()
	srv := httptest.NewServer(buildMux(eps))
	t.Cleanup(srv.Close)
	return srv
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode healthz body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want \"ok\"", body["status"])
	}
}

func TestMetrics(t *testing.T) {
	srv := newTestServer(t)

	// Make a request first so counters are non-zero.
	http.Get(srv.URL + "/token-bucket") //nolint:errcheck

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		t.Error("missing Content-Type header")
	}

	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	for _, want := range []string{
		"rate_limiter_requests_total",
		"rate_limiter_active_keys",
		`algorithm="TokenBucket"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics body missing %q", want)
		}
	}
}

func TestStats(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var stats []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats body: %v", err)
	}
	if len(stats) != 5 {
		t.Errorf("stats entries = %d, want 5 (one per algorithm)", len(stats))
	}
}

// TestEndpoints_RateLimitHeaders verifies every algorithm endpoint returns the
// X-RateLimit-Algorithm and X-RateLimit-Remaining headers on a normal request.
func TestEndpoints_RateLimitHeaders(t *testing.T) {
	srv := newTestServer(t)

	routes := []string{
		"/token-bucket",
		"/fixed-window",
		"/leaky-bucket",
		"/sliding-log",
		"/sliding-counter",
	}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			resp, err := http.Get(srv.URL + route)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body) //nolint:errcheck
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("first request: status = %d, want 200", resp.StatusCode)
			}
			if resp.Header.Get("X-RateLimit-Algorithm") == "" {
				t.Error("missing X-RateLimit-Algorithm header")
			}
			if resp.Header.Get("X-RateLimit-Remaining") == "" {
				t.Error("missing X-RateLimit-Remaining header")
			}
		})
	}
}

// TestEndpoints_429AfterLimit fires requests until the limiter exhausts and
// verifies that subsequent requests receive HTTP 429.
// Uses /fixed-window because its limit=10 is deterministic and resets on a hard boundary.
func TestEndpoints_429AfterLimit(t *testing.T) {
	srv := newTestServer(t)
	client := srv.Client()

	const limit = windowLimit // 10, defined in main.go

	// Exhaust the quota for a single IP (the test server IP will be 127.0.0.1).
	for i := 0; i < limit; i++ {
		resp, err := client.Get(srv.URL + "/fixed-window")
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 before limit", i+1, resp.StatusCode)
		}
	}

	// The next request must be rejected.
	resp, err := client.Get(srv.URL + "/fixed-window")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("request after limit: status = %d, want 429", resp.StatusCode)
	}
}
